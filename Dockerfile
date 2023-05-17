FROM golang:1.20 as build

WORKDIR /build

RUN wget https://github.com/open-telemetry/opentelemetry-collector/releases/download/cmd%2Fbuilder%2Fv0.71.0/ocb_0.71.0_linux_amd64
RUN chmod +x ocb_0.71.0_linux_amd64
COPY sample-builder-config.yaml .

RUN ./ocb_0.71.0_linux_amd64 --config=sample-builder-config.yaml

FROM amazonlinux:2

WORKDIR /opt/asserts
COPY --from=build /build/asserts-otel-collector /opt/asserts
COPY sample-collector-config.yaml /etc/asserts/collector-config.yaml

EXPOSE 8888
EXPOSE 8889
EXPOSE 9465
EXPOSE 4317
EXPOSE 14278
EXPOSE 14250

ENTRYPOINT /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config.yaml