# Enterprise Wiki - multi-stage build producing a small, non-root image.
# The build stages run on the build machine (BUILDPLATFORM) and cross-compile
# for the target (multi-arch images: linux/amd64, linux/arm64).
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci --no-audit --no-fund
COPY web web
COPY plugins plugins
RUN cd web && node build.mjs

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN go run ./tools/notices
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/mtsonline/enterprisewiki/internal/version.Version=${VERSION}" \
      -o /out/ewiki ./cmd/ewiki

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata poppler-utils \
 && adduser -D -H -u 10001 ewiki \
 && mkdir -p /data && chown ewiki /data
COPY --from=backend /out/ewiki /usr/local/bin/ewiki
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
USER ewiki
WORKDIR /data
ENV EWIKI_CONFIG=/data/config.yaml
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve"]
