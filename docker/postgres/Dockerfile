# PostgreSQL for Enterprise Wiki with automatic major version upgrades.
#
# Data of each major version lives in its own directory inside the volume
# (/var/lib/postgresql/ewiki/pg<major>). When the image is updated to a new
# major version, the entrypoint finds the data of the older version, starts
# it with the old binaries (installed below), writes a dump to /backups,
# restores it into a new cluster, compares all tables row by row (counts)
# and only then switches over. The old data directory stays untouched.
# Renovate updates the version below; CI tests the upgrade from the previous
# major version before an update is merged.
FROM postgres:18-alpine

# server binaries of older major versions (as far as Alpine still ships
# them) – needed to read and dump the old data
RUN set -eux; \
    for v in 14 15 16 17 18 19 20 21 22; do \
      [ "$v" -lt "${PG_MAJOR%%.*}" ] || continue; \
      apk add --no-cache "postgresql$v" "postgresql$v-contrib" >/dev/null 2>&1 && echo "old binaries: postgresql$v" || echo "not available: postgresql$v"; \
    done; \
    ls -d /usr/libexec/postgresql* 2>/dev/null || true

COPY ewiki-postgres-entrypoint.sh /usr/local/bin/
ENV PGDATA_ROOT=/var/lib/postgresql/ewiki
VOLUME ["/backups"]
ENTRYPOINT ["ewiki-postgres-entrypoint.sh"]
CMD ["postgres"]
