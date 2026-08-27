MISE := mise exec --

.PHONY: run devauth dev

run: ## Run the app against the Auth0 tenant configured in .env
	$(MISE) go run ./cmd/chores -addr=:8080 -db=../chores-data/chores.db

devauth: ## Run the local test OAuth2 identity provider standalone
	$(MISE) go run ./cmd/devauth -client-id=devclient -client-secret=devsecret

dev: ## Run the app wired to the local devauth server, plus devauth itself
	$(MISE) go run ./cmd/devauth -client-id=devclient -client-secret=devsecret & \
	devauth_pid=$$!; \
	trap 'kill $$devauth_pid 2>/dev/null' EXIT; \
	AUTH0_DOMAIN=http://localhost:9999 AUTH0_CLIENT_ID=devclient \
	AUTH0_CLIENT_SECRET=devsecret \
	$(MISE) go run ./cmd/chores -addr=:8080 -db=../chores-data/chores.db
