VERSION ?= dev
LDFLAGS := -s -w -X github.com/DuncanDoyle/snellius/pkg/version.Version=$(VERSION)
GOLANGCI_LINT_VERSION := v2.15.0
KGW_VERSION ?= v2.4.7
GWAPI_VERSION ?= v1.6.1
SECOND_INSTALL ?= 0
UI_EXTRA_ARGS ?=
PORT ?= 18080
UI_URL ?= http://127.0.0.1:$(PORT)
ACTIONLINT_VERSION := v1.7.12
# The timing regression gate's benchmarks (D-044): warm and cold recompute, confirm tick, analyzer stage.
BENCH_AB ?= ^Benchmark(Compute|ColdCompute|ConfirmTick|AnalyzerStage)$$/^(large|warm-scope|memoized-scope)
BENCH_AB_COUNT ?= 5

.PHONY: help test lint actionlint build build-core image-core web shell-pack example-web run e2e e2e-down bench bench-ab bench-stream
help:
	@echo "make test   run the Go tests"
	@echo "make lint   run go vet and golangci-lint"
	@echo "make bench  run the scale benchmarks and budget tests, after the Go version, machine and host load"
	@echo "           SNELLIUS_BENCH_TIMING=report logs the absolute time budgets instead of failing (CI; D-044)"
	@echo "make bench-ab BASE_DIR=<checkout>  run the timing benchmarks of BASE_DIR and this tree interleaved; fail when one is > 15 % slower"
	@echo "make bench-stream  run the 10-minute sustained-stream budget, 5 runs per world, about 105 min (make bench runs the 60 s -short one)"
	@echo "make bench-specedit  run the 10-minute spec-edit stream, 5 runs per world, about 105 min (make bench runs the 60 s -short one)"
	@echo "make actionlint  lint the GitHub workflows (shellcheck on run: blocks when it is on PATH)"
	@echo "make web    build the SPA and copy it into internal/webui/dist"
	@echo "make build  make web, then build bin/snellius with the SPA embedded"
	@echo "make build-core  make web, then build bin/snellius-core: the Gateway API core alone, with class-only installs on"
	@echo "make image-core  build the core-only image snellius-core:dev (IMAGE_CORE sets the tag)"
	@echo "make shell-pack  build the shell package into web/packages/shell/snellius-shell-<version>.tgz"
	@echo "make example-web  typecheck and test examples/downstream/web against the packed shell"
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
	@echo "timing budgets: $${SNELLIUS_BENCH_TIMING:-reference} (D-044)"
	@# -short shortens only the API stream; no engine or cache budget may read testing.Short().
	@# The budget tests run once; the benchmarks five times.
	go test -tags bench -v -short -run 'Budget' -count 1 ./pkg/engine ./pkg/cache
	go test -tags bench -run '^$$' -bench . -benchmem -count 5 ./pkg/engine ./pkg/cache
	@# The API budgets take 10,000 samples per case, so they run once; -short keeps each of the 5 stream runs per world at 60 s.
	go test -tags bench -v -short -run 'Budget' -count 1 -timeout 75m ./pkg/api
	go test -tags bench -run '^$$' -bench . -benchmem -count 5 ./pkg/api
bench-ab:
	@test -n "$(BASE_DIR)" || { echo "BASE_DIR is required"; exit 2; }
	go run ./internal/benchab -base "$(BASE_DIR)" -head . -pkg ./pkg/engine -bench '$(BENCH_AB)' -count $(BENCH_AB_COUNT) -out "$(or $(BENCH_AB_OUT),.)"
bench-stream:
	go test -tags bench -v -run 'TestBudgetSustainedStream' -count 1 -timeout 150m ./pkg/api
.PHONY: bench-specedit
bench-specedit:
	go test -tags bench -v -run 'TestBudgetSpecEditStream' -count 1 -timeout 150m ./pkg/api
lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --build-tags e2e ./...
actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
web:
	cd web && npm ci && npm run build
	rm -rf internal/webui/dist && cp -r web/app/dist internal/webui/dist && touch internal/webui/dist/.gitkeep
	find internal/webui/dist -name '*.map' -delete
shell-pack:
	cd web && { [ -d node_modules ] || npm ci; }
	rm -rf web/packages/shell/dist web/packages/shell/*.tgz
	cd web && npm run build -w @snellius/shell
	@# Packs a staged copy without the @snellius/source condition: the tarball ships dist only.
	d=$$(mktemp -d) && cp -r web/packages/shell/dist "$$d/" && \
	  node -e 'const fs=require("fs"),[src,dst]=process.argv.slice(1),p=JSON.parse(fs.readFileSync(src));delete p.exports["."]["@snellius/source"];fs.writeFileSync(dst,JSON.stringify(p,null,2)+"\n")' web/packages/shell/package.json "$$d/package.json" && \
	  (cd "$$d" && npm pack --pack-destination "$(CURDIR)/web/packages/shell") && rm -rf "$$d"
example-web: shell-pack
	@# Installs the tarball, not the workspace source; peers are listed in its package.json, so npm need not resolve optional peer chains.
	rm -rf examples/downstream/web/node_modules/@snellius
	npm --prefix examples/downstream/web install --no-save --no-package-lock --legacy-peer-deps $$(ls web/packages/shell/snellius-shell-*.tgz)
	cd examples/downstream/web && npx tsc --noEmit && npx vitest run
build: web
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/snellius ./cmd/snellius
build-core: web
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/snellius-core ./cmd/snellius-core
IMAGE_CORE ?= snellius-core:dev
image-core:
	docker build --build-arg CMD=snellius-core -t $(IMAGE_CORE) .
run:
	go run ./cmd/snellius --kubeconfig $${KUBECONFIG:-$$HOME/.kube/config}
e2e:
	KGW_VERSION=$(KGW_VERSION) GWAPI_VERSION=$(GWAPI_VERSION) SECOND_INSTALL=$(SECOND_INSTALL) PORT=$(PORT) UI_EXTRA_ARGS="$(UI_EXTRA_ARGS)" bash test/e2e/setup.sh
	SECOND_INSTALL=$(SECOND_INSTALL) UI_URL=$(UI_URL) go test -tags e2e ./test/e2e/... -v -count=1
	cd web && { [ -d node_modules ] || npm ci; } && npx playwright install chromium
	cd web && SECOND_INSTALL=$(SECOND_INSTALL) UI_URL=$(UI_URL) npx playwright test -c e2e/playwright.config.ts
e2e-down:
	bash test/e2e/setup.sh down
