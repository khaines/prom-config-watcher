FROM alpine:3.7

COPY prom-config-watcher /


ENTRYPOINT ["/prom-config-watcher"]