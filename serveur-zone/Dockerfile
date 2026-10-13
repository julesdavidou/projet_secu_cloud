FROM --platform=$BUILDPLATFORM golang:1.27.2 AS builder
WORKDIR /src
# Build from repository root: allocator imports the shared placement module.
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build cd serveur-zone && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/serveur-zone ./app

FROM alpine:3.22
RUN addgroup -S -g 10001 zone && adduser -S -u 10001 -G zone zone && mkdir /data && chown zone:zone /data
COPY --from=builder /out/serveur-zone /usr/local/bin/serveur-zone
USER 10001:10001
ENV APP_PORT=8000
EXPOSE 8000
HEALTHCHECK --interval=5s --timeout=2s CMD wget -q -O - http://127.0.0.1:$APP_PORT/health || exit 1
ENTRYPOINT ["serveur-zone"]
