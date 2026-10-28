VERSION ?= dev
LDFLAGS := -s -w -X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=$(VERSION)
GOLANGCI_LINT_VERSION := v2.15.0
KGW_VERSION ?= v2.4.7
GWAPI_VERSION ?= v1.6.1
SECOND_INSTALL ?= 0
UI_EXTRA_ARGS ?=
PORT ?= 18080
UI_URL ?= http://127.0.0.1:$(PORT)
ACTIONLINT_VERSION := v1.7.12

.PHONY: help test lint actionlint build web run e2e e2e-down bench bench-stream
help:
	@echo "make test   run the Go tests"
	@echo "make lint   run go vet and golangci-lint"
	@echo "make bench  run the scale benchmarks and budget tests, after the Go version, machine and host load"
	@echo "make bench-stream  run the 10-minute sustained-stream budget (make bench runs the 60 s -short one)"
	@echo "make actionlint  lint the GitHub workflows (shellcheck on run: blocks when it is on PATH)"
	@echo "make web    build the SPA and copy it into internal/webui/dist"
	@echo "make build  make web, then build bin/kgateway-ui with the SPA embedded"
	@echo "make run    run the server from source (placeholder page unless make web ran)"
	@echo "make e2e    kind cluster with kgateway KGW_VERSION, then the API and Playwright tests (installs web deps and Chromium if needed)"
	@echo "           CI pairs: KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1, KGW_VERSION=v2.3.11 GWAPI_VERSION=v1.5.1; PORT sets the local port"
	@echo "           UI_EXTRA_ARGS=--poll-interval=10m also runs the forced-disagreement test"
	@echo "make e2e-down  delete the e2e kind cluster"
test:
	go test ./...
bench:
	@go version
	@echo "machine: $$(uname -sm), $$(getconf _NPROCESSORS_ONLN) CPUs"
	@echo "load: $$(uptime)"
	@# -short shortens only the API stream; no engine or cache budget may read testing.Short().
	@# The budget tests run once; the benchmarks five times.
	go test -tags bench -v -short -run 'Budget' -count 1 ./pkg/engine ./pkg/cache
	go test -tags bench -run '^$$' -bench . -benchmem -count 5 ./pkg/engine ./pkg/cache
	@# The API budgets take 10,000 samples per case, so they run once; -short keeps the stream at 60 s.
	go test -tags bench -v -short -run 'Budget' -count 1 -timeout 20m ./pkg/api
	go test -tags bench -run '^$$' -bench . -benchmem -count 5 ./pkg/api
bench-stream:
	go test -tags bench -v -run 'TestBudgetSustainedStream' -count 1 -timeout 30m ./pkg/api
lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --build-tags e2e ./...
actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
web:
	cd web && npm ci && npm run build
	rm -rf internal/webui/dist && cp -r web/app/dist internal/webui/dist && touch internal/webui/dist/.gitkeep
	find internal/webui/dist -name '*.map' -delete
build: web
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/kgateway-ui ./cmd/kgateway-ui
run:
	go run ./cmd/kgateway-ui --kubeconfig $${KUBECONFIG:-$$HOME/.kube/config}
e2e:
	KGW_VERSION=$(KGW_VERSION) GWAPI_VERSION=$(GWAPI_VERSION) SECOND_INSTALL=$(SECOND_INSTALL) PORT=$(PORT) UI_EXTRA_ARGS="$(UI_EXTRA_ARGS)" bash test/e2e/setup.sh
	SECOND_INSTALL=$(SECOND_INSTALL) UI_URL=$(UI_URL) go test -tags e2e ./test/e2e/... -v -count=1
	cd web && { [ -d node_modules ] || npm ci; } && npx playwright install chromium
	cd web && SECOND_INSTALL=$(SECOND_INSTALL) UI_URL=$(UI_URL) npx playwright test -c e2e/playwright.config.ts
e2e-down:
	bash test/e2e/setup.sh down
