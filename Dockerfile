# syntax=docker/dockerfile:1
# Frontend: built once on the build platform; output is architecture-independent.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
COPY web/packages/shell/package.json packages/shell/
COPY web/app/package.json app/
RUN npm ci
COPY web/ ./
RUN npm run build

# Backend: cross-compiled for the target platform with the SPA embedded.
FROM --platform=$BUILDPLATFORM golang:1.26 AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/webui/dist
COPY --from=web /src/web/app/dist internal/webui/dist
# Source maps must not be embedded (see internal/webui/embed_test.go).
RUN find internal/webui/dist -name '*.map' -delete
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/DuncanDoyle/snellius/pkg/version.Version=${VERSION}" -o /out/snellius ./cmd/snellius

FROM gcr.io/distroless/static:nonroot
# image.source links the ghcr package to the repo.
LABEL org.opencontainers.image.source="https://github.com/DuncanDoyle/snellius" \
      org.opencontainers.image.description="Read-only UI for Kubernetes Gateway API, built for kgateway" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=go /out/snellius /snellius
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/snellius"]
