VERSION ?= dev
LDFLAGS := -s -w -X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=$(VERSION)
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: help test lint build web run
help:
	@echo "make test   run the Go tests"
	@echo "make lint   run go vet and golangci-lint"
	@echo "make web    build the SPA and copy it into internal/webui/dist"
	@echo "make build  make web, then build bin/kgateway-ui with the SPA embedded"
	@echo "make run    run the server from source (placeholder page unless make web ran)"
test:
	go test ./...
lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...
web:
	cd web && npm ci && npm run build
	rm -rf internal/webui/dist && cp -r web/app/dist internal/webui/dist && touch internal/webui/dist/.gitkeep
	find internal/webui/dist -name '*.map' -delete
build: web
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/kgateway-ui ./cmd/kgateway-ui
run:
	go run ./cmd/kgateway-ui --kubeconfig $${KUBECONFIG:-$$HOME/.kube/config}
