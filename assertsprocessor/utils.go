package assertsprocessor

import "go.opentelemetry.io/collector/pdata/ptrace"

func computeLatency(span ptrace.Span) float64 {
	return float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9
}
