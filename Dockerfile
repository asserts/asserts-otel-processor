FROM amazonlinux:2

WORKDIR /opt/asserts
COPY asserts-otel-collector /opt/asserts

EXPOSE 8888
EXPOSE 8889
EXPOSE 9465
EXPOSE 4317
EXPOSE 14278
EXPOSE 14250

ENTRYPOINT /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config.yaml