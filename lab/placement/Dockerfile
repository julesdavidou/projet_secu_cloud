FROM --platform=$BUILDPLATFORM golang:1.27.2 AS source
WORKDIR /src
COPY . .

FROM source AS verify
RUN test -z "$(gofmt -l pkg/placement lab/placement serveur-zone/app)"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd lab/placement && go test ./... && go test -race ./... && go vet ./...
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && go test ./... && go test -race ./... && go vet ./...

FROM source AS builder
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd lab/placement && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/placement-lab ./cmd/lab
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/serveur-zone ./app

FROM alpine:3.22 AS runtime
RUN addgroup -g 10001 -S placement && adduser -u 10001 -S -G placement placement && mkdir /data && chown placement:placement /data
COPY --from=builder /out/placement-lab /usr/local/bin/placement-lab
COPY --from=builder /out/serveur-zone /usr/local/bin/serveur-zone
USER 10001:10001
EXPOSE 8088
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD wget -q -O - http://127.0.0.1:8088/health || exit 1
ENTRYPOINT ["placement-lab"]

FROM runtime AS zone
ENTRYPOINT ["serveur-zone"]
