# mise pins the toolchain where it's installed; where it isn't (agent
# sandboxes, a plain CI runner) the same targets run against whatever go and
# node are on PATH instead of failing with "mise: not found".
MISE := $(if $(shell command -v mise 2>/dev/null),mise exec --,)

.PHONY: run devauth dev check e2e update-material-symbols test-data

run: ## Run the app against the Auth0 tenant configured in .env
	$(MISE) go run ./cmd/chores -addr=:8080 -db=chores.db

devauth: ## Run the local test OAuth2 identity provider standalone
	$(MISE) go run ./cmd/devauth -client-id=devclient -client-secret=devsecret

dev: ## Run the app wired to the local devauth server, plus devauth itself
	$(MISE) go run ./cmd/devauth -client-id=devclient -client-secret=devsecret & \
	devauth_pid=$$!; \
	trap 'kill $$devauth_pid 2>/dev/null' EXIT; \
	AUTH0_DOMAIN=http://localhost:9999 AUTH0_CLIENT_ID=devclient \
	AUTH0_CLIENT_SECRET=devsecret \
	$(MISE) go run ./cmd/chores -addr=:8080 -db=chores.db

check: ## Build, vet, gofmt, unit tests and JS syntax — everything short of a browser
	$(MISE) go build ./...
	$(MISE) go vet ./...
	@out=$$(gofmt -l . | grep -v '^gen/'); [ -z "$$out" ] || { echo "gofmt needed:"; echo "$$out"; exit 1; }
	$(MISE) go test ./...
	@for f in web/*.js; do $(MISE) node --check $$f || exit 1; done

e2e: ## Browser smoke test against a throwaway server (scripts/e2e.sh)
	$(MISE) ./scripts/e2e.sh

update-material-symbols: ## Refresh web/material-symbols.json with the latest Material Symbols icon names
	$(MISE) ./scripts/update-material-symbols.sh

test-data: ## Seed a family, children, a task and completions for manual testing (needs `make dev` running; pass extra flags via ARGS="--children Anna,Erik")
	$(MISE) node .claude/skills/verify-ui/seed-demo-data.js $(ARGS)
