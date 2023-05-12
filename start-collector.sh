#!/bin/bash

if [ "$TRACE_STORE" == "OTLP-HTTP" ]; then
    /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config-otlphttp.yaml
elif [ "$TRACE_STORE" == "AWS-XRAY" ]; then
    /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config-aws-xray.yaml
elif [ "$TRACE_STORE" == "GOOGLE-CLOUDTRACE" ]; then
    /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config-cloudtrace.yaml
else
    /opt/asserts/asserts-otel-collector --config /etc/asserts/collector-config-otlp.yaml
fi


