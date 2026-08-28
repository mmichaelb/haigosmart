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
FROM scratch

COPY haigosmartd /haigosmartd

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
