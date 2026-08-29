# The unattended server, and nothing else.
#
# scratch is deliberate and it is reachable: the only outbound connection is a
# plaintext TCP dial to the MQTT broker, so no CA bundle is needed; the TLS
# material the lamps see is generated at runtime and a key that cannot be
# persisted is already non-fatal; and every target builds with CGO_ENABLED=0, so
# there is no libc to supply.
#
# The cost is real and worth knowing: there is no shell, so `docker exec` gives
# you nothing. Diagnosis is the JSON record stream on standard output — which is
# what it was built for. See docs/releasing.md.
# scratch has no filesystem to inherit from, so a volume mounted at /data is
# created root-owned and the server — running as 65534 — cannot write it. The
# registry save then fails on every attempt: not fatal, but a permanent warning
# and no state across restarts. This stage exists solely to carry an empty /data
# with the right ownership into the final image, so a named volume works with no
# host-side setup. Nothing from it survives except that directory.
FROM busybox:stable AS prep
RUN mkdir -p /data && chown 65534:65534 /data

FROM scratch

# GoReleaser stages each platform's binary under $TARGETOS/$TARGETARCH in the
# build context, so one Dockerfile serves every platform. TARGETOS and TARGETARCH
# are supplied by buildx; they have to be declared to be usable.
ARG TARGETOS
ARG TARGETARCH
COPY ${TARGETOS}/${TARGETARCH}/haigosmartd /haigosmartd
COPY --from=prep --chown=65534:65534 /data /data

# The registry file and the self-signed TLS key live here. Both are caches: the
# configured lamp set is authoritative, so losing this volume costs only the
# last-known state, which lamps report again on connecting.
VOLUME ["/data"]

# The default registry path resolves through the user configuration directory,
# which does not exist in scratch. Without this the server falls back to a
# relative path in an unwritable root and warns on every save, forever, while
# otherwise appearing to work.
ENV HAIGOSMART_REGISTRY=/data/registry.json

# No terminal here, and headless serves exactly the lamps it is configured with.
ENV HAIGOSMART_HEADLESS=true

EXPOSE 1883

# nobody. There is no /etc/passwd in scratch, so the id is numeric.
USER 65534:65534

ENTRYPOINT ["/haigosmartd"]
