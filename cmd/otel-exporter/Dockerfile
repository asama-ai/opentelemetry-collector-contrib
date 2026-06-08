# syntax=docker/dockerfile:1.4

FROM alpine:latest@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375 AS prep
RUN apk --update add ca-certificates

FROM scratch

ARG USER_UID=10001
ARG USER_GID=10001
USER ${USER_UID}:${USER_GID}

COPY --from=prep /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY otel-exporter /
COPY --from=normalize-registries asama-bmc-events.json /etc/bmc/asama-bmc-events.json
COPY --from=normalize-registries mappings/ /etc/bmc/mappings/
COPY --from=fault-registries fault-eligible-events.json /etc/bmc/fault-eligible-events.json
EXPOSE 4317 55680 55679
ENTRYPOINT ["/otel-exporter"]
CMD ["--config", "/etc/otel/config.yaml"]
