# Enterprise Wiki - multi-stage build of the non-root images "lite" (default)
# and "full" (docker build --target full).
# The build stages run on the build machine (BUILDPLATFORM) and cross-compile
# for the target (multi-arch images: linux/amd64, linux/arm64).
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci --no-audit --no-fund
COPY web web
COPY plugins plugins
RUN cd web && node build.mjs

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS backend
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
# retried: the module proxy occasionally drops connections
RUN for i in 1 2 3; do go mod download && exit 0; sleep 10; done; exit 1
COPY . .
COPY --from=web /src/web/dist web/dist
RUN go run ./tools/notices
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/mtsonline/enterprisewiki/internal/version.Version=${VERSION}" \
      -o /out/ewiki ./cmd/ewiki

# ---------------------------------------------------------------- full
# Full image (ghcr.io/…/enterprisewiki:full): additionally the source code
# and the Go and Node toolchains, so plugins installed in the administration
# or copied into /data/plugins are built into /data/bin (see
# internal/pluginmgr/image.go). Built for the target platform (native
# esbuild, Go toolchain of the target).
FROM node:26-alpine AS node

FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS full
RUN apk add --no-cache ca-certificates tzdata poppler-utils git bash libstdc++ libgcc libatomic \
 && adduser -D -H -u 10001 ewiki \
 && mkdir -p /data && chown ewiki /data
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
WORKDIR /opt/ewiki/src
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci --no-audit --no-fund && npm cache clean --force
COPY go.mod go.sum ./
# modules of the core, served as a local module proxy: builds work offline
# unless a plugin needs further modules
ENV GOMODCACHE=/opt/ewiki/gomod GOTOOLCHAIN=local
RUN for i in 1 2 3; do go mod download && exit 0; sleep 10; done; exit 1
COPY . .
RUN chmod -R a+rX /opt/ewiki
COPY --from=backend /out/ewiki /usr/local/bin/ewiki
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
USER ewiki
WORKDIR /data
ENV EWIKI_CONFIG=/data/config.yaml EWIKI_DOCKER=1 EWIKI_IMAGE=full EWIKI_DATA=/data \
    EWIKI_SOURCE=/opt/ewiki/src EWIKI_GOPROXY_FILE=/opt/ewiki/gomod/cache/download
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10m CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve"]

# ---------------------------------------------------------------- lite
# Lite image (default, ghcr.io/…/enterprisewiki:latest): only the binary.
# Plugins can be enabled and disabled; installing needs the full image.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS lite
RUN apk add --no-cache ca-certificates tzdata poppler-utils \
 && adduser -D -H -u 10001 ewiki \
 && mkdir -p /data && chown ewiki /data
COPY --from=backend /out/ewiki /usr/local/bin/ewiki
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
USER ewiki
WORKDIR /data
ENV EWIKI_CONFIG=/data/config.yaml EWIKI_DOCKER=1 EWIKI_IMAGE=lite EWIKI_DATA=/data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve"]
