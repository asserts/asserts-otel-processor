package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
	"testing"
)

func TestCompileRequestContextRegexpsSuccess(t *testing.T) {
	logger, _ := zap.NewProduction()
	matcher := spanMatcher{}
	err := matcher.compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &[]*MatcherDto{
			{
				AttrName: "attribute1",
				Regex:    "Foo",
			},
			{
				AttrName: "attribute2",
				Regex:    "Bar.+",
			},
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, matcher.spanAttrMatchers)
	assert.Equal(t, 2, len(matcher.spanAttrMatchers))

	regExp := matcher.spanAttrMatchers[0].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute1", matcher.spanAttrMatchers[0].attrName)
	assert.True(t, regExp.MatchString("Foo"))

	regExp = matcher.spanAttrMatchers[1].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute2", matcher.spanAttrMatchers[1].attrName)
	assert.True(t, regExp.MatchString("Bart"))
}

func TestCompileRequestContextRegexpsFailure(t *testing.T) {
	logger, _ := zap.NewProduction()
	matcher := spanMatcher{}
	err := matcher.compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &[]*MatcherDto{
			{
				AttrName: "attribute1",
				Regex:    "+",
			},
			{
				AttrName: "attribute2",
				Regex:    "Bar.+",
			},
		},
	})
	assert.NotNil(t, err)
}

func TestGetExpMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()

	compile, _ := regexp.Compile("https?://.+?((/[^/?]+){1,2}).*")
	matcher := spanMatcher{
		spanAttrMatchers: []*spanAttrMatcher{
			{
				attrName: "http.url",
				regex:    compile,
			},
		},
	}

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo")
	assert.Equal(t, "/foo", matcher.getRequest(&testSpan))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo?a=b")
	assert.Equal(t, "/foo", matcher.getRequest(&testSpan))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar?a=b")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar/baz")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar/baz?a=b")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan))
}

func TestGetExpNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetName("BackgroundJob")
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	matcher := spanMatcher{
		spanAttrMatchers: []*spanAttrMatcher{
			{
				attrName: "http.url",
				regex:    compile,
			},
		},
	}

	value := matcher.getRequest(&testSpan)
	assert.Equal(t, "BackgroundJob", value)
}
