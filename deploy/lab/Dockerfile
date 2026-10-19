FROM --platform=$BUILDPLATFORM golang:1.27.2 AS source
WORKDIR /src
COPY . .
FROM source AS verify
RUN test -z "$(gofmt -l pkg/controllers internal/edgecontrol internal/storage/verified*.go cmd/central-controller cmd/edge-bridge cmd/lab-admin cmd/edge-agent serveur-zone/app)"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go version && go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd lab/placement && go test ./... && go test -race ./... && go vet ./...
FROM source AS builder
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/ ./cmd/central-controller ./cmd/edge-bridge ./cmd/edge-agent ./cmd/lab-admin
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/serveur-zone ./app
FROM alpine:3.22 AS runtime
RUN addgroup -g 10001 -S caribou && adduser -u 10001 -S -G caribou caribou && mkdir /data && chown caribou:caribou /data
COPY --from=builder /out/ /usr/local/bin/
USER 10001:10001
EXPOSE 8088
HEALTHCHECK --interval=2s --timeout=2s --retries=30 CMD wget -q -O - http://127.0.0.1:8088/ready || exit 1
CMD ["central-controller"]
