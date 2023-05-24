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

	assert.Equal(t, []*ptrace.Span{&rootSpan, &entrySpan, &exitSpan}, ts.getSpans())
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

	assert.Equal(t, &rootSpan, ts1.getMainSpan())
	assert.Equal(t, &entrySpan, ts2.getMainSpan())
	assert.Equal(t, &exitSpan, ts3.getMainSpan())
}

func TestHasError(t *testing.T) {
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

	assert.False(t, ts1.hasError())
	rootSpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts1.hasError())

	assert.False(t, ts2.hasError())
	entrySpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts2.hasError())

	assert.False(t, ts3.hasError())
	exitSpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts3.hasError())
}
