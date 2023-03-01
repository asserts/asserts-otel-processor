# Asserts Trace Processor

| Status                   |           |
| Stability                | [beta]    |
| Supported pipeline types | traces    |

A trace processor with the following features
* Generates span metrics for selected spans. Spans can be selected by regexp based conditions on span attributes
* Samples error traces and slow traces. Uses latency baselines from Asserts to identify slow traces.
* Samples normal traces
* Rate limits traces 

# Building the Collector
See the `sample-builder-config.yaml`. The collector builder binary can be downloaded from [here](https://github.com/open-telemetry/opentelemetry-collector/releases).
The collector can be built as follows
```
ocb_0.71.0_darwin_amd64 --config=sample-builder-config.yaml
```

# Configuration
```
processors:
  assertsprocessor:
    asserts_server: 
      endpoint: http://localhost:8030
      user: 
      password:
    asserts_env: dev
    asserts_site: us-west-2
    span_attribute_match_regex:
      "rpc.system": "aws-api"
      "rpc.service": "(Sqs)|(DynamoDb)"
    request_context_regex:
      "http.url": "https?://.+(/.+)"
    attributes_as_metric_labels:
     - "rpc.system"
     - "rpc.service"
     - "rpc.method"
     - "aws.table.name"
     - "aws.queue.url"
    # Default threshold to identify slow trace
    sampling_latency_threshold_seconds: 0.5
    # Max traces per request
    max_traces_per_minute: 100
    # Max traces per container per minute. This overrides the per request limit
    max_traces_per_minute_per_container: 100
    normal_trace_sampling_rate_minutes: 5
    trace_flush_interval_seconds: 15
```

# Running the collector
```
./build/asserts-otel-collector --config sample-collector-config.yaml
```

