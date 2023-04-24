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
	matcher := requestContextBuilderImpl{
		logger: logger,
	}
	err := matcher.compileRequestContextRegexps(&Config{
		RequestContextExps: map[string][]*MatcherDto{
			"namespace#service": {
				{
					AttrName:    "attribute1",
					Regex:       "Foo",
					SpanKind:    "Client",
					Replacement: "$1",
				},
				{
					AttrName: "attribute2",
					SpanKind: "Server",
					Regex:    "Bar.+",
				},
			},
			"default": {
				{
					AttrName: "attribute3",
					SpanKind: "Server",
					Regex:    "Baz.+",
				},
			},
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, matcher.requestConfigs)
	assert.Equal(t, 2, len(matcher.requestConfigs))
	assert.Equal(t, 2, len(matcher.requestConfigs["namespace#service"]))
	assert.Equal(t, 1, len(matcher.requestConfigs["default"]))

	regExp := matcher.requestConfigs["namespace#service"][0].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute1", matcher.requestConfigs["namespace#service"][0].attrName)
	assert.Equal(t, "Client", matcher.requestConfigs["namespace#service"][0].spanKind)
	assert.True(t, regExp.MatchString("Foo"))

	regExp = matcher.requestConfigs["namespace#service"][1].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute2", matcher.requestConfigs["namespace#service"][1].attrName)
	assert.Equal(t, "Server", matcher.requestConfigs["namespace#service"][1].spanKind)
	assert.True(t, regExp.MatchString("Bart"))

	regExp = matcher.requestConfigs["default"][0].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute3", matcher.requestConfigs["default"][0].attrName)
	assert.Equal(t, "Server", matcher.requestConfigs["default"][0].spanKind)
	assert.True(t, regExp.MatchString("Bazz"))
}

func TestCompileRequestContextRegexpsFailure(t *testing.T) {
	logger, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: logger,
	}
	err := matcher.compileRequestContextRegexps(&Config{
		RequestContextExps: map[string][]*MatcherDto{
			"namespace#service": {
				{
					SpanKind:    "Server",
					AttrName:    "attribute1",
					Regex:       "+",
					Replacement: "$1",
				},
			},
			"default": {
				{
					SpanKind: "Server",
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
	testSpan.SetKind(ptrace.SpanKindServer)
	serverExp, _ := regexp.Compile("https?://.+?((/[^/?]+){1,2}).*")
	clientExp, _ := regexp.Compile("(.+)")
	production, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: production,
		requestConfigs: map[string][]*requestConfigCompiled{
			"namespace#service": {
				{
					spanKind:    "Server",
					attrName:    "http.url",
					regex:       serverExp,
					replacement: "$1",
				},
				{
					spanKind:    "Client",
					attrName:    "http.url",
					regex:       clientExp,
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

	testSpan.SetKind(ptrace.SpanKindClient)
	testSpan.Attributes().PutStr("http.url", "https://some.domain.com/foo")
	assert.Equal(t, "https://some.domain.com/foo", matcher.getRequest(&testSpan, "namespace#service"))
}

func TestGetRequestMatchMultipleGroups(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetKind(ptrace.SpanKindServer)
	compile1, _ := regexp.Compile("http://user:8080(/check)/anonymous-.*")
	compile2, _ := regexp.Compile("http://cart:8080(/add)/[0-9]+/([A-Z]+)/[0-9]+")
	production, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: production,
		requestConfigs: map[string][]*requestConfigCompiled{
			"robot-shop#user": {
				{
					spanKind:    "Server",
					attrName:    "http.url",
					regex:       compile1,
					replacement: "$1/#val",
				},
			},
			"robot-shop#cart": {
				{
					spanKind:    "Server",
					attrName:    "http.url",
					regex:       compile2,
					replacement: "$1/$2",
				},
			},
		},
	}

	testSpan.Attributes().PutStr("http.url", "http://user:8080/check/anonymous-1")
	assert.Equal(t, "/check/#val", matcher.getRequest(&testSpan, "robot-shop#user"))

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/add/123/TOY/2")
	assert.Equal(t, "/add/TOY", matcher.getRequest(&testSpan, "robot-shop#cart"))

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/check/anonymous-1")
	assert.Equal(t, "", matcher.getRequest(&testSpan, "robot-shop#cart"))
}

func TestGetRequestNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetName("BackgroundJob")
	testSpan.SetKind(ptrace.SpanKindServer)
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	production, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: production,
		requestConfigs: map[string][]*requestConfigCompiled{
			"default": {
				{
					spanKind:    "Server",
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

func TestSpanMatcherIsUpdated(t *testing.T) {
	prevExps := map[string][]*MatcherDto{
		"namespace#service": {
			{
				SpanKind:    "Server",
				AttrName:    "attribute1",
				Regex:       "(Foo)",
				Replacement: "$1",
			},
			{
				SpanKind: "Server",
				AttrName: "attribute2",
				Regex:    "(Bar).+",
			},
		},
		"default": {
			{
				SpanKind: "Server",
				AttrName: "attribute3",
				Regex:    "(Baz).+",
			},
		},
	}
	currConfig := &Config{
		RequestContextExps: prevExps,
	}
	currentExps := map[string][]*MatcherDto{
		"namespace#service": {
			{
				SpanKind:    "Server",
				AttrName:    "attribute1",
				Regex:       "(Foo)",
				Replacement: "$1",
			},
			{
				SpanKind: "Server",
				AttrName: "attribute2",
				Regex:    "(Bar).+",
			},
		},
		"default": {
			{
				SpanKind: "Server",
				AttrName: "attribute3",
				Regex:    "(Baz).+",
			},
		},
	}
	newConfig := &Config{
		RequestContextExps: currentExps,
	}

	logger, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: logger,
	}

	assert.False(t, matcher.isUpdated(currConfig, newConfig))

	currentExps["namespace#service"][1].Replacement = "$2"
	assert.True(t, matcher.isUpdated(currConfig, newConfig))

	delete(currentExps, "default")
	assert.True(t, matcher.isUpdated(currConfig, newConfig))
}

func TestSpanMatcherOnUpdateSuccess(t *testing.T) {
	config := &Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					SpanKind:    "Server",
					AttrName:    "attribute1",
					Regex:       "(Foo)",
					Replacement: "$1",
				},
			},
		},
	}

	logger, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: logger,
	}

	assert.Nil(t, matcher.requestConfigs)
	err := matcher.onUpdate(config)
	assert.Nil(t, err)
	assert.NotNil(t, matcher.requestConfigs)
	assert.Equal(t, 1, len(matcher.requestConfigs))
}

func TestSpanMatcherOnUpdateError(t *testing.T) {
	config := &Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					SpanKind:    "Server",
					AttrName:    "attribute1",
					Regex:       "+",
					Replacement: "$1",
				},
			},
		},
	}

	logger, _ := zap.NewProduction()
	matcher := requestContextBuilderImpl{
		logger: logger,
	}

	assert.Nil(t, matcher.requestConfigs)
	err := matcher.onUpdate(config)
	assert.NotNil(t, err)
	assert.Nil(t, matcher.requestConfigs)
}
