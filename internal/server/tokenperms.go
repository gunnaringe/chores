package server

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

// Personal access token permissions. A token copied into Home Assistant or
// onto a wall display shouldn't be able to delete the family, so each token
// carries the set of things it may do, and PersonalTokenOrAuth checks the
// RPC against it before the request reaches the service — the same
// perimeter-allowlist shape as dashboardAllowedMethods. Login sessions are
// never limited by any of this.

// tokenPermissionNames is how permissions are stored in
// personal_access_tokens.permissions (space-separated), and the order the
// UI lists them in.
var tokenPermissionNames = []struct {
	perm v1.TokenPermission
	name string
}{
	{v1.TokenPermission_TOKEN_PERMISSION_READ, "read"},
	{v1.TokenPermission_TOKEN_PERMISSION_TICK_CHORES, "tick"},
	{v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES, "chores"},
	{v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHILDREN, "children"},
	{v1.TokenPermission_TOKEN_PERMISSION_PAYOUTS, "payouts"},
	{v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN, "family"},
}

// defaultTokenPermissions is what a token gets when it's created without
// naming any: enough for a wall display, nothing that changes the family.
var defaultTokenPermissions = []v1.TokenPermission{
	v1.TokenPermission_TOKEN_PERMISSION_READ,
	v1.TokenPermission_TOKEN_PERMISSION_TICK_CHORES,
}

// allTokenPermissions is what a legacy token (permissions IS NULL) has.
func allTokenPermissions() []v1.TokenPermission {
	perms := make([]v1.TokenPermission, len(tokenPermissionNames))
	for i, p := range tokenPermissionNames {
		perms[i] = p.perm
	}
	return perms
}

// tokenMethodPermission maps every RPC a token may call to the permission
// it needs. An RPC missing from here is refused to every token —
// TestTokenMethodPermission_CoversEveryRPC makes adding an RPC a
// deliberate choice. The ones left out on purpose are token management
// (a token minting a broader token would undo the point), accepting
// invitations and push subscriptions, which only make sense in a browser.
var tokenMethodPermission = map[string]v1.TokenPermission{
	"ListFamilies":        v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListUsers":           v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListTasks":           v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListTaskOccurrences": v1.TokenPermission_TOKEN_PERMISSION_READ,
	"GetChildSummary":     v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListChildSummaries":  v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListMonthlyEarnings": v1.TokenPermission_TOKEN_PERMISSION_READ,
	"ListPayouts":         v1.TokenPermission_TOKEN_PERMISSION_READ,
	"GetMyMembership":     v1.TokenPermission_TOKEN_PERMISSION_READ,
	"GetPushConfig":       v1.TokenPermission_TOKEN_PERMISSION_READ,

	"CompleteTask":   v1.TokenPermission_TOKEN_PERMISSION_TICK_CHORES,
	"UncompleteTask": v1.TokenPermission_TOKEN_PERMISSION_TICK_CHORES,

	"CreateTask": v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES,
	"UpdateTask": v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES,
	"DeleteTask": v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES,

	"CreateUser":  v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHILDREN,
	"RemoveChild": v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHILDREN,

	"CreatePayout": v1.TokenPermission_TOKEN_PERMISSION_PAYOUTS,

	"CreateFamily":       v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"UpdateFamily":       v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"DeleteFamily":       v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"GetDashboardConfig": v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"SetupDashboard":     v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"DisableDashboard":   v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"UpdateUser":         v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"LeaveFamily":        v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"CreateInvitation":   v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"ListInvitations":    v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
	"RevokeInvitation":   v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN,
}

const choresServicePrefix = "/chores.v1.ChoresService/"

// permissionForPath returns the permission a token needs for an HTTP
// request path, and false if no token may call it. gRPC reflection is
// read-only and only describes the API, so READ covers it (grpcurl needs it
// to call anything at all).
func permissionForPath(path string) (v1.TokenPermission, bool) {
	if strings.HasPrefix(path, "/grpc.reflection.") {
		return v1.TokenPermission_TOKEN_PERMISSION_READ, true
	}
	method, ok := strings.CutPrefix(path, choresServicePrefix)
	if !ok {
		return 0, false
	}
	perm, ok := tokenMethodPermission[method]
	return perm, ok
}

func encodeTokenPermissions(perms []v1.TokenPermission) string {
	var names []string
	for _, p := range tokenPermissionNames {
		if slices.Contains(perms, p.perm) {
			names = append(names, p.name)
		}
	}
	return strings.Join(names, " ")
}

// decodeTokenPermissions ignores names it doesn't know, so a database
// written by a newer build never grants more than this build understands.
func decodeTokenPermissions(s string) []v1.TokenPermission {
	var perms []v1.TokenPermission
	for _, name := range strings.Fields(s) {
		for _, p := range tokenPermissionNames {
			if p.name == name {
				perms = append(perms, p.perm)
			}
		}
	}
	return perms
}

// validateTokenPermissions turns a create request's permissions into the
// set to store: the defaults when none are given, otherwise exactly what
// was asked for, minus duplicates.
func validateTokenPermissions(requested []v1.TokenPermission) ([]v1.TokenPermission, error) {
	if len(requested) == 0 {
		return defaultTokenPermissions, nil
	}
	known := allTokenPermissions()
	var perms []v1.TokenPermission
	for _, p := range requested {
		if !slices.Contains(known, p) {
			return nil, fmt.Errorf("unknown permission %v", p)
		}
		if !slices.Contains(perms, p) {
			perms = append(perms, p)
		}
	}
	return perms, nil
}

func writePermissionDenied(w http.ResponseWriter, r *http.Request, message string) {
	ew := connect.NewErrorWriter()
	err := connect.NewError(connect.CodePermissionDenied, errors.New(message))
	if ew.IsSupported(r) {
		_ = ew.Write(w, r, err)
		return
	}
	http.Error(w, message, http.StatusForbidden)
}

// permissionLabel is the name a refusal message uses, matching what the
// API keys page shows (in English — errors aren't translated).
func permissionLabel(p v1.TokenPermission) string {
	switch p {
	case v1.TokenPermission_TOKEN_PERMISSION_READ:
		return "Read"
	case v1.TokenPermission_TOKEN_PERMISSION_TICK_CHORES:
		return "Tick off tasks"
	case v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES:
		return "Manage tasks"
	case v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHILDREN:
		return "Manage children"
	case v1.TokenPermission_TOKEN_PERMISSION_PAYOUTS:
		return "Payouts"
	case v1.TokenPermission_TOKEN_PERMISSION_FAMILY_ADMIN:
		return "Family admin"
	}
	return p.String()
}
