package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

func (s *Server) CreateFamily(ctx context.Context, req *connect.Request[v1.CreateFamilyRequest]) (*connect.Response[v1.CreateFamilyResponse], error) {
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	// Creating a family also makes the caller its founding parent, bound to
	// their login identity. A login can found or join any number of
	// families (see AcceptInvitation), so there's nothing to check here —
	// this is always a brand-new family, never one the identity could
	// already be bound to.
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO families (id, name, created_at) VALUES (?, ?, ?)`,
		id, name, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create family: %w", err))
	}

	parentName := req.Msg.GetParentName()
	if parentName == "" {
		parentName = identity.Name
	}
	if parentName == "" {
		parentName = identity.Email
	}
	if parentName == "" {
		parentName = "Parent"
	}
	uid := newID()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject, email) VALUES (?, ?, ?, 'parent', ?, ?, ?)`,
		uid, id, parentName, formatTime(now), identity.Sub, identity.Email,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bind founding parent: %w", err))
	}

	return connect.NewResponse(&v1.CreateFamilyResponse{
		Family: &v1.Family{Id: id, Name: name, CreatedAt: timestampPB(now)},
	}), nil
}

func (s *Server) ListFamilies(ctx context.Context, _ *connect.Request[v1.ListFamiliesRequest]) (*connect.Response[v1.ListFamiliesResponse], error) {
	// A login only ever sees the family/families it's bound to.
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.name, f.created_at FROM families f
		 JOIN users u ON u.family_id = f.id
		 WHERE u.auth_subject = ?
		 ORDER BY f.created_at`,
		identity.Sub,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var families []*v1.Family
	for rows.Next() {
		var f v1.Family
		var createdAt string
		if err := rows.Scan(&f.Id, &f.Name, &createdAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		f.CreatedAt = timestampPB(t)
		families = append(families, &f)
	}
	return connect.NewResponse(&v1.ListFamiliesResponse{Families: families}), nil
}

func (s *Server) getFamily(ctx context.Context, familyID string) (*v1.Family, error) {
	var f v1.Family
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM families WHERE id = ?`, familyID).
		Scan(&f.Id, &f.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	f.CreatedAt = timestampPB(t)
	return &f, nil
}

// DeleteFamily removes the family and, via cascading foreign keys,
// everything tied to it — users, tasks, completions, payouts, invitations,
// push subscriptions.
func (s *Server) DeleteFamily(ctx context.Context, req *connect.Request[v1.DeleteFamilyRequest]) (*connect.Response[v1.DeleteFamilyResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM families WHERE id = ?`, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete family: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
	}
	return connect.NewResponse(&v1.DeleteFamilyResponse{}), nil
}

func (s *Server) UpdateFamily(ctx context.Context, req *connect.Request[v1.UpdateFamilyRequest]) (*connect.Response[v1.UpdateFamilyResponse], error) {
	familyID := req.Msg.GetFamilyId()
	name := strings.TrimSpace(req.Msg.GetName())
	if familyID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and name are required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE families SET name = ? WHERE id = ?`, name, familyID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update family: %w", err))
	}
	family, err := s.getFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.UpdateFamilyResponse{Family: family}), nil
}
