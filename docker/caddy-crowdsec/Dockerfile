# Caddy with the CrowdSec bouncer (HTTP handler and AppSec), for
# docker-compose.crowdsec.yml. The official Caddy image plus two modules of
# github.com/hslatman/caddy-crowdsec-bouncer (Apache-2.0), built with xcaddy.
# Versions are pinned (Renovate keeps them current).
ARG CADDY_VERSION=2.11.9
FROM caddy:${CADDY_VERSION}-builder AS builder
ARG BOUNCER_VERSION=v0.15.0
# no file capability on the binary (XCADDY_SETCAP): the proxy runs with all
# capabilities dropped and no-new-privileges, where a binary with file
# capabilities cannot start; ports below 1024 need none inside containers
RUN XCADDY_SETCAP=0 xcaddy build \
	--with github.com/hslatman/caddy-crowdsec-bouncer/http@${BOUNCER_VERSION} \
	--with github.com/hslatman/caddy-crowdsec-bouncer/appsec@${BOUNCER_VERSION}

FROM caddy:${CADDY_VERSION}
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
# docker-compose.yml runs the proxy as the wiki's user anyway; not root by default
USER 1000:1000
