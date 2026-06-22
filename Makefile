# Tasky build targets. Run inside the Nix dev shell (direnv loads it) so go,
# node, and goose are on PATH. See docs/adr/ for the decisions here.

BACKEND  := backend
FRONTEND := frontend
BIN      := bin/tasky

.PHONY: dev dev-api dev-web build run openapi test clean

## dev: run the Go API and the Vite dev server (Vite proxies API to Go)
dev:
	@$(MAKE) -j2 dev-api dev-web

dev-api:
	cd $(BACKEND) && go run ./cmd/tasky

dev-web: $(FRONTEND)/node_modules
	cd $(FRONTEND) && npm run dev

## openapi: regenerate the OpenAPI spec from the huma handlers
openapi:
	cd $(BACKEND) && go run ./cmd/tasky --openapi > api/openapi.yaml

## build: build the Go binary and the frontend into public/
build: openapi $(FRONTEND)/node_modules
	cd $(BACKEND) && go build -o ../$(BIN) ./cmd/tasky
	cd $(FRONTEND) && npm run build

## run: production — run the Go binary serving public/ + the API
run: build
	./$(BIN)

## test: run the backend HTTP-integration test suite (needs Postgres)
test:
	cd $(BACKEND) && go test ./...

$(FRONTEND)/node_modules: $(FRONTEND)/package.json
	cd $(FRONTEND) && npm install
	@touch $(FRONTEND)/node_modules

clean:
	rm -rf public bin $(BACKEND)/api/openapi.yaml
