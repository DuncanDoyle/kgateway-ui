# Contributing to Snellius

Thanks for helping. Snellius is an observability UI for kgateway, licensed under Apache-2.0.

## Report a bug or ask for a feature

Open an [issue](https://github.com/DuncanDoyle/snellius/issues/new/choose). For a wrong finding or a wrong computed outcome, include the finding code, your kgateway and Snellius versions, and the YAML of the objects involved, with secrets removed. Report security issues privately, as [SECURITY.md](SECURITY.md) explains.

## Build and test

You need Go 1.26 and Node 22. The end-to-end suite also needs Docker, [kind](https://kind.sigs.k8s.io), `kubectl` and `helm`.

```sh
make build   # the SPA and the Go binary, with the SPA embedded
make test    # Go tests
make lint    # linters
make e2e     # kind cluster with kgateway, then the API and Playwright tests
make help    # every target
```

`./bin/snellius --kubeconfig ~/.kube/config` serves the UI on http://127.0.0.1:8080 against your current cluster.

## Pull requests

- Keep a PR to one change, and say what it changes and how you tested it.
- Add or update tests for the behaviour you change. CI runs the Go tests, the linters, the web tests and the kind suite against several kgateway versions.
- API v1 (`/api/v1/...`) and the page URLs are stable: add fields, never change or remove them.
- A change users can see (pages, findings, flags, chart values, install steps) also updates the user docs at [snellius.dev](https://snellius.dev), in [DuncanDoyle/snellius.dev](https://github.com/DuncanDoyle/snellius.dev). Link that PR from yours.
- The design, decisions and roadmap live in [`docs/`](docs/). For a larger change, open an issue first so we can agree on the approach.
