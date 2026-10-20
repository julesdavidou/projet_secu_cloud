# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.2 AS dependencies
WORKDIR /src
COPY go.mod go.sum ./
COPY lab/placement/go.mod lab/placement/go.sum ./lab/placement/
COPY serveur-zone/go.mod serveur-zone/go.sum ./serveur-zone/
# Exported dependency layers survive ephemeral CI runners, unlike cache mounts alone.
RUN go mod download && cd lab/placement && go mod download && cd /src/serveur-zone && go mod download
FROM dependencies AS source
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY lab/placement/ ./lab/placement/
COPY serveur-zone/ ./serveur-zone/
FROM source AS verify
COPY . .
# Existing archives or Windows working copies may still contain CRLF.
RUN find . -name '*.go' -exec sed -i 's/\r$//' {} +
RUN test -z "$(gofmt -l pkg/controllers internal/edgecontrol internal/storage/verified*.go cmd/central-controller cmd/edge-bridge cmd/lab-admin cmd/edge-agent serveur-zone/app)"
RUN --mount=type=cache,target=/go/pkg/mod,from=dependencies,source=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go version && go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod,from=dependencies,source=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod,from=dependencies,source=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd lab/placement && go test ./... && go test -race ./... && go vet ./...
FROM source AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN --mount=type=cache,target=/go/pkg/mod,from=dependencies,source=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOARM=${TARGETVARIANT#v}; export GOARM=${GOARM:-7}; CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/ ./cmd/central-controller ./cmd/edge-bridge ./cmd/edge-agent ./cmd/lab-admin ./cmd/tls-run
RUN --mount=type=cache,target=/go/pkg/mod,from=dependencies,source=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && GOARM=${TARGETVARIANT#v}; export GOARM=${GOARM:-7}; CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/serveur-zone ./app
FROM alpine:3.22 AS runtime
RUN addgroup -g 10001 -S caribou && adduser -u 10001 -S -G caribou caribou && mkdir /data && chown caribou:caribou /data
COPY --from=builder /out/ /usr/local/bin/
ARG VERSION=dev
LABEL org.opencontainers.image.revision=$VERSION
LABEL org.opencontainers.image.source="https://github.com/julesdavidou/projet_secu_cloud"
USER 10001:10001
EXPOSE 8088
HEALTHCHECK --interval=2s --timeout=2s --retries=30 CMD wget -q -O - http://127.0.0.1:8088/ready || exit 1
CMD ["central-controller"]
