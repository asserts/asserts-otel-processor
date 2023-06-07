package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"testing"
)

func TestGetSpans(t *testing.T) {
	rootSpan := ptrace.NewSpan()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	entrySpan := ptrace.NewSpan()
	entrySpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	exitSpan := ptrace.NewSpan()
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	internalSpan := ptrace.NewSpan()
	internalSpan.SetSpanID([8]byte{4, 1, 3, 4, 5, 6, 7, 8})

	ts := traceSegment{
		rootSpan:      &rootSpan,
		entrySpans:    []*ptrace.Span{&entrySpan},
		exitSpans:     []*ptrace.Span{&exitSpan},
		internalSpans: []*ptrace.Span{&internalSpan},
	}

	assert.Equal(t, []*ptrace.Span{&rootSpan, &entrySpan, &exitSpan}, ts.getNonInternalSpans())
}

func TestGetMainSpan(t *testing.T) {
	rootSpan := ptrace.NewSpan()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	entrySpan := ptrace.NewSpan()
	entrySpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	exitSpan := ptrace.NewSpan()
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})

	ts1 := traceSegment{
		rootSpan:   &rootSpan,
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts2 := traceSegment{
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts3 := traceSegment{
		exitSpans: []*ptrace.Span{&exitSpan},
	}
	ts4 := traceSegment{}

	assert.Equal(t, &rootSpan, ts1.getMainSpan())
	assert.Equal(t, &entrySpan, ts2.getMainSpan())
	assert.Equal(t, &exitSpan, ts3.getMainSpan())
	assert.Nil(t, ts4.getMainSpan())
}

func TestGetSpanCount(t *testing.T) {
	span := ptrace.NewSpan()

	ts1 := &traceSegment{
		rootSpan:   &span,
		entrySpans: []*ptrace.Span{&span, &span},
		exitSpans:  []*ptrace.Span{&span},
	}
	ts2 := &traceSegment{
		entrySpans: []*ptrace.Span{&span},
		exitSpans:  []*ptrace.Span{&span, &span},
	}
	ts3 := &traceSegment{
		exitSpans:     []*ptrace.Span{&span, &span},
		internalSpans: []*ptrace.Span{&span, &span},
	}
	ts4 := &traceSegment{}

	assert.Equal(t, 4, ts1.getSpanCount())
	assert.Equal(t, 3, ts2.getSpanCount())
	assert.Equal(t, 4, ts3.getSpanCount())
	assert.Equal(t, 0, ts4.getSpanCount())
}
