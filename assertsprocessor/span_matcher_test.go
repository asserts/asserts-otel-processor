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
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName:    "attribute1",
					Regex:       "Foo",
					Replacement: "$1",
				},
				{
					AttrName: "attribute2",
					Regex:    "Bar.+",
				},
			},
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, matcher.spanAttrMatchers)
	assert.Equal(t, 1, len(matcher.spanAttrMatchers))
	assert.Equal(t, 2, len(matcher.spanAttrMatchers["default"]))

	regExp := matcher.spanAttrMatchers["default"][0].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute1", matcher.spanAttrMatchers["default"][0].attrName)
	assert.True(t, regExp.MatchString("Foo"))

	regExp = matcher.spanAttrMatchers["default"][1].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute2", matcher.spanAttrMatchers["default"][1].attrName)
	assert.True(t, regExp.MatchString("Bart"))
}

func TestCompileRequestContextRegexpsFailure(t *testing.T) {
	logger, _ := zap.NewProduction()
	matcher := spanMatcher{}
	err := matcher.compileRequestContextRegexps(logger, &Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName:    "attribute1",
					Regex:       "+",
					Replacement: "$1",
				},
				{
					AttrName: "attribute2",
					Regex:    "Bar.+",
				},
			},
		},
	})
	assert.NotNil(t, err)
}

func TestGetRequestMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()

	compile, _ := regexp.Compile("https?://.+?((/[^/?]+){1,2}).*")
	matcher := spanMatcher{
		spanAttrMatchers: map[string][]*spanAttrMatcher{
			"default": {
				{
					attrName:    "http.url",
					regex:       compile,
					replacement: "$1",
				},
			},
		},
	}

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo")
	assert.Equal(t, "/foo", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo?a=b")
	assert.Equal(t, "/foo", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar?a=b")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar/baz")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar/baz?a=b")
	assert.Equal(t, "/foo/bar", matcher.getRequest(&testSpan, "namespace#service"))
}

func TestGetRequestMatchMultipleGroups(t *testing.T) {
	testSpan := ptrace.NewSpan()

	compile1, _ := regexp.Compile("http://user:8080(/check)/anonymous-.*")
	compile2, _ := regexp.Compile("http://user:8080(/add)/[0-9]+/([A-Z]+)/[0-9]+")
	matcher := spanMatcher{
		spanAttrMatchers: map[string][]*spanAttrMatcher{
			"default": {
				{
					attrName:    "http.url",
					regex:       compile1,
					replacement: "$1/#val",
				},
				{
					attrName:    "http.url",
					regex:       compile2,
					replacement: "$1/$2",
				},
			},
		},
	}

	testSpan.Attributes().PutStr("http.url", "http://user:8080/check/anonymous-1")
	assert.Equal(t, "/check/#val", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "http://user:8080/add/123/TOY/2")
	assert.Equal(t, "/add/TOY", matcher.getRequest(&testSpan, "namespace#service"))

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/check/anonymous-1")
	assert.Equal(t, "", matcher.getRequest(&testSpan, "namespace#service"))
}

func TestGetRequestNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetName("BackgroundJob")
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	matcher := spanMatcher{
		spanAttrMatchers: map[string][]*spanAttrMatcher{
			"default": {
				{
					attrName:    "http.url",
					regex:       compile,
					replacement: "$1",
				},
			},
		},
	}

	value := matcher.getRequest(&testSpan, "namespace#service")
	assert.Equal(t, "BackgroundJob", value)
}
