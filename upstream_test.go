package assets

import (
	"testing"

	gohttp "net/http"

	"github.com/valyala/fasthttp"
	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/log"
)

func init() {
	Config.CacheRoot = "tests/"
}

func Test_NewUpstream_NoDefaultCaching(t *testing.T) {
	up, err := NewUpstream("up1_local", &upstreamConfig{
		BaseURL: "https://src.goblgobl.com/assets/",
		Buffers: &buffer.Config{
			Count: 2,
			Min:   4096,
			Max:   4096,
		},
		Caching: []upstreamCacheConfig{
			upstreamCacheConfig{Status: 200, TTL: 60},
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, up.baseURL, "https://src.goblgobl.com/assets/")
	assert.Equal(t, up.cacheRoot, "tests/up1_local/")

	// default TTL is set to 300
	assert.Equal(t, up.defaultTTL, 300)
	assert.Equal(t, len(up.ttls), 1)
	assert.Equal(t, up.ttls[200], 60)
}

func Test_NewUpstream_WithDefaultCaching(t *testing.T) {
	up, err := NewUpstream("up1_local", &upstreamConfig{
		BaseURL: "https://src.goblgobl.com/assets/",
		Buffers: &buffer.Config{},
		Caching: []upstreamCacheConfig{
			upstreamCacheConfig{Status: 0, TTL: 45},
			upstreamCacheConfig{Status: 200, TTL: 30},
			upstreamCacheConfig{Status: 201, TTL: 32},
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, up.defaultTTL, 45)
	assert.Equal(t, len(up.ttls), 2)
	assert.Equal(t, up.ttls[200], 30)
	assert.Equal(t, up.ttls[201], 32)
}

func Test_Upstream_NextRequestId(t *testing.T) {
	seen := make(map[string]struct{}, 60)

	u := Upstream{requestId: 1}
	for i := 0; i < 20; i++ {
		seen[u.NextRequestId()] = struct{}{}
	}

	u = Upstream{requestId: 100}
	for i := 0; i < 20; i++ {
		seen[u.NextRequestId()] = struct{}{}
	}

	Config.InstanceId += 1
	u = Upstream{requestId: 1}
	for i := 0; i < 20; i++ {
		seen[u.NextRequestId()] = struct{}{}
	}

	assert.Equal(t, len(seen), 60)
}

func Test_Upstream_CachePath(t *testing.T) {
	u := &Upstream{cacheRoot: "/tmp/x/"}
	assert.Equal(t, u.CachePath("hello_world"), "/tmp/x/he/hello_world")
}

func Test_Upstream_IsFileCached(t *testing.T) {
	u := testUpstream1()
	path, exists := u.IsFileCached("does_not_exist", nil)
	assert.False(t, exists)
	assert.Equal(t, path, "tests/up1_local/do/does_not_exist")

	path, exists = u.IsFileCached(EncodePath("sample1.css"), nil)
	assert.True(t, exists)
	assert.Equal(t, path, "tests/up1_local/c2/c2FtcGxlMS5jc3M")
}

func Test_Upstream_LoadLocal(t *testing.T) {
	u := testUpstream1()

	wirteLocal(NewEnv(u), "sample1.css", &RemoteResponse{
		buffer:       buffer.Containing([]byte("sample1 content"), 0),
		status:       199,
		contentType:  "assets/sample1",
		cacheControl: "private;max-age=9",
	})

	res, path := u.LoadLocal(EncodePath("sample1.css"), nil)
	assert.Equal(t, path, "tests/up1_local/c2/c2FtcGxlMS5jc3M")

	conn := &fasthttp.RequestCtx{}
	res.Write(conn, log.Noop{})
	body := request.Res(t, conn).
		ExpectStatus(199).
		Header("Content-Type", "assets/sample1").
		Header("Cache-Control", "private;max-age=9").
		Body
	assert.Equal(t, body, "sample1 content")

	res, path = u.LoadLocal(EncodePath("does_not_exist"), nil)
	assert.Equal(t, path, "tests/up1_local/ZG/ZG9lc19ub3RfZXhpc3Q")
	assert.Nil(t, res)
}

func Test_Upstream_CalculateExpired(t *testing.T) {
	createUpstream := func(defaultTTL uint32, ttls ...int) *Upstream {
		lookup := make(map[int]int32, len(ttls)/2)
		for i := 0; i < len(ttls); i += 2 {
			lookup[ttls[i]] = int32(ttls[i+1])
		}
		return &Upstream{defaultTTL: defaultTTL, ttls: lookup}
	}

	createResponse := func(statusCode int, cc ...string) *gohttp.Response {
		header := make(gohttp.Header, 1)
		if len(cc) > 0 {
			header["Cache-Control"] = cc
		}
		return &gohttp.Response{
			Header:     header,
			StatusCode: statusCode,
		}
	}

	u1 := createUpstream(399)
	// uses default
	assert.Equal(t, u1.calculateExpires(createResponse(200)), 399)
	// uses default
	assert.Equal(t, u1.calculateExpires(createResponse(200)), 399)
	// uses header
	assert.Equal(t, u1.calculateExpires(createResponse(200, "max-age=60")), 60)
	// invalid header, uses default
	assert.Equal(t, u1.calculateExpires(createResponse(200, "max-age=")), 399)
	// uses header
	assert.Equal(t, u1.calculateExpires(createResponse(200, "public, max-age=3")), 3)

	u2 := createUpstream(499, 200, 60, 404, -32)
	// uses default
	assert.Equal(t, u2.calculateExpires(createResponse(201)), 499)
	// uses header
	assert.Equal(t, u2.calculateExpires(createResponse(200, "max-age=9")), 9)
	// uses status-specific configuration
	assert.Equal(t, u2.calculateExpires(createResponse(200)), 60)
	// uses status-specific configuration
	assert.Equal(t, u2.calculateExpires(createResponse(404)), 32)
	// uses status-specific configuration (even with a cache-control header
	// because it's set to a negative, which means "force")
	assert.Equal(t, u2.calculateExpires(createResponse(404, "max-age=9")), 32)
}

func testUpstream1() *Upstream {
	return testUpstream("up1_local")
}

func testUpstream2() *Upstream {
	return testUpstream("up2_local")
}

func testUpstream(name string) *Upstream {
	up, err := NewUpstream(name, &upstreamConfig{
		BaseURL: "https://src.goblgobl.com/assets/",
		Buffers: &buffer.Config{
			Count: 2,
			Min:   4096,
			Max:   4096,
		},
	})
	if err != nil {
		panic(err)
	}
	return up
}
