MISE := mise exec --

.PHONY: run devauth dev update-material-symbols test-data

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

update-material-symbols: ## Refresh web/material-symbols.json with the latest Material Symbols icon names
	$(MISE) ./scripts/update-material-symbols.sh

test-data: ## Seed a family, children, a task and completions for manual testing (needs `make dev` running; pass extra flags via ARGS="--children Anna,Erik")
	$(MISE) node .claude/skills/verify-ui/seed-demo-data.js $(ARGS)
