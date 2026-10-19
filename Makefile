VERSION ?= dev
LDFLAGS := -s -w -X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=$(VERSION)
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: test lint build web run
test:
	go test ./...
lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...
web:
	cd web && npm ci && npm run build
	rm -rf internal/webui/dist && cp -r web/app/dist internal/webui/dist
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/kgateway-ui ./cmd/kgateway-ui
run:
	go run ./cmd/kgateway-ui --kubeconfig $${KUBECONFIG:-$$HOME/.kube/config}
