<div align="center">
<img src="web/icons/logo.png" width="96" height="96" alt="">

# Chores

**A family allowance and chore tracker.**

Choose between running yourself or using the hosted version for free. \
★ [Self host](#quick-start) ★ \
★ [chores.apphub.casa](https://chores.apphub.casa) ★ \
★ [ukepenger.apphub.casa](https://ukepenger.apphub.casa) ★

Single Go binary, embedded web UI, SQLite storage. \
Available in English, Norwegian (Bokmål and Nynorsk), and Swedish. \
Dashboard mode for a wall-mounted tablet. \
Installable as a Progressive Web App. \
gRPC and REST-ish API through Buf Connect — [SDK](https://buf.build/apphub/chores). \
Home Assistant app (coming soon).

<p>
  <img src="web/screenshots/today-en.webp" width="230" alt="The Today tab, showing each child's tasks and what they've earned">
  <img src="web/screenshots/tasks-en.webp" width="230" alt="The Tasks tab, listing chores with their price and schedule">
  <img src="web/screenshots/balance-en.webp" width="230" alt="The Balance tab, showing what's owed and a pay-out form">
</p>

</div>

---

The easiest way to use this is the hosted version above — no payment
required, anyone's welcome to sign up. Prefer your own data and uptime?
See Quick Start below. The code is [Apache-2.0 licensed](LICENSE), so
self-hosting or forking it is explicitly fine.

For everything else — features, hosting, authentication, family
membership, the kiosk dashboard, the API, deploying schema changes — see
[docs/APP.md](docs/APP.md).

After making this, these other projects turned up that might also be
worth a look:
- https://github.com/donetick/donetick
- https://github.com/liftedkilt/openchore
- https://github.com/ccpk1/choreops
- https://github.com/caspii/dinkydash

## Quick Start

### Run from source

```bash
go run ./cmd/chores -addr=:8080 -db=chores.db
```

Then open http://localhost:8080. A login is always required — set
`AUTH0_DOMAIN`, `AUTH0_CLIENT_ID`, and `AUTH0_CLIENT_SECRET` (via `.env`,
env vars, or `-auth0-*` flags), or run `cmd/devauth` alongside it for local
testing without a real Auth0 tenant. See
[Authentication](docs/APP.md#authentication) for details.

### Container

```bash
docker build -t chores .
docker run -p 8080:8080 -v "$PWD/data:/data" --env-file .env chores
```

### Build locally

Uses [mise](https://mise.jdx.dev/) to pin toolchain versions (Go, Node,
buf) — see `mise.toml`. `make dev` runs the app against a local test
identity provider, no real Auth0 tenant needed.

## API

SDKs are generated from the schema and published to
[Buf BSR](https://buf.build/apphub/chores), pushed automatically whenever
the schema changes on `main`. The web UI itself talks the same API, as
JSON. See
[External API access](docs/APP.md#external-api-access) for personal access
tokens and gRPC reflection.

## Contributing

Issues and PRs are welcome — open an issue before a large change so it can
be discussed first.

## Licence

Apache 2.0

## Support Chores

Consider leaving a star, or telling a neighbour about the project, if you
find it useful.
