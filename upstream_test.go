package assets

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	up, err := NewUpstream("up2_local", &upstreamConfig{
		BaseURL: "https://src.goblgobl.com/assets/",
		Buffers: &buffer.Config{
			Count: 2,
			Min:   4096,
			Max:   4096,
		},
		Caching: []upstreamCacheConfig{
			upstreamCacheConfig{Status: 200, TTL: 60},
		},
		VipsTransforms: map[string][]string{"large": []string{"--size", "200x100"}},
	})
	assert.Nil(t, err)
	assert.Equal(t, up.baseURL, "https://src.goblgobl.com/assets/")

	assert.Equal(t, string(up.cacheRoot), "tests/up2_local/")

	// default TTL is set to 300
	assert.Equal(t, up.defaultTTL, 300)
	assert.Equal(t, len(up.ttls), 1)
	assert.Equal(t, up.ttls[200], 60)

	assert.Equal(t, len(up.vipsTransforms), 1)
	assert.Equal(t, up.vipsTransforms["large"][0], "--size")
	assert.Equal(t, up.vipsTransforms["large"][1], "200x100")
}

func Test_NewUpstream_WithDefaultCaching(t *testing.T) {
	up, err := NewUpstream("up2_local", &upstreamConfig{
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

func Test_Upstream_LocalPath(t *testing.T) {
	u := &Upstream{cacheRoot: []byte("up1/cache/")}
	assert.Equal(t, u.LocalResPath("hello_world", ".test"), "up1/cache/aG/aGVsbG9fd29ybGQ.test.res")
	assert.Equal(t, u.LocalResPath("hello_world", ""), "up1/cache/aG/aGVsbG9fd29ybGQ.res")
}

func Test_Upstream_LocalImagePath(t *testing.T) {
	u := &Upstream{cacheRoot: []byte("up1/cache/")}
	metaPath, imagePath := u.LocalImagePath("hello_world2", ".jpg", []byte("thumb100"))
	assert.Equal(t, metaPath, "up1/cache/aG/aGVsbG9fd29ybGQy_thumb100.jpg.res")
	assert.Equal(t, imagePath, "up1/cache/aG/aGVsbG9fd29ybGQy_thumb100.jpg")
}

func Test_Upstream_LoadLocalResponse(t *testing.T) {
	u := testUpstream2()

	rr := BuildRemoteResponse().
		Body("sample1 content").
		Status(199).
		ContentType("assets/sample1").
		CacheControl("private;max-age=9").
		Response()

	localPath := writeLocal(NewEnv(u), "sample1.css", rr)
	res := u.LoadLocalResponse(localPath, nil, false)

	conn := &fasthttp.RequestCtx{}
	res.Write(conn, log.Noop{})
	body := request.Res(t, conn).
		ExpectStatus(199).
		Header("Content-Type", "assets/sample1").
		Header("Cache-Control", "private;max-age=9").
		Body
	assert.Equal(t, body, "sample1 content")

	localPath = u.LocalResPath("does_not_exist", "")
	res = u.LoadLocalResponse(localPath, nil, false)
	assert.Nil(t, res)
}

func Test_Upstream_CalculateTTL(t *testing.T) {
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
	assert.Equal(t, u1.calculateTTL(createResponse(200)), 399)
	// uses default
	assert.Equal(t, u1.calculateTTL(createResponse(200)), 399)
	// uses header
	assert.Equal(t, u1.calculateTTL(createResponse(200, "max-age=60")), 60)
	// invalid header, uses default
	assert.Equal(t, u1.calculateTTL(createResponse(200, "max-age=")), 399)
	// uses header
	assert.Equal(t, u1.calculateTTL(createResponse(200, "public, max-age=3")), 3)

	u2 := createUpstream(499, 200, 60, 404, -32)
	// uses default
	assert.Equal(t, u2.calculateTTL(createResponse(201)), 499)
	// uses header
	assert.Equal(t, u2.calculateTTL(createResponse(200, "max-age=9")), 9)
	// uses status-specific configuration
	assert.Equal(t, u2.calculateTTL(createResponse(200)), 60)
	// uses status-specific configuration
	assert.Equal(t, u2.calculateTTL(createResponse(404)), 32)
	// uses status-specific configuration (even with a cache-control header
	// because it's set to a negative, which means "force")
	assert.Equal(t, u2.calculateTTL(createResponse(404, "max-age=9")), 32)
}

func Test_Upstream_OriginImageCheck_DoesNotExist(t *testing.T) {
	up := testUpstream2()
	env := NewEnv(up)

	localPath := up.LocalResPath("does_not_exist", "")
	res, expires, err := up.OriginImageCheck(localPath, env)
	assert.Nil(t, res)
	assert.Nil(t, err)
	assert.Equal(t, expires, 0)
}

func Test_Upstream_OriginImageCheck_HasNonImage(t *testing.T) {
	up := testUpstream2()
	env := NewEnv(up)

	rr := BuildRemoteResponse().Response()
	localPath := writeLocal(env, "has_local_non_image", rr)
	res, expires, err := up.OriginImageCheck(localPath, env)
	assert.Nil(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, expires, 0)
}

func Test_Upstream_OriginImageCheck_HasImage(t *testing.T) {
	up := testUpstream2()
	env := NewEnv(up)

	rr := BuildRemoteResponse().Expires(100).Image().Response()
	localPath := writeLocal(env, "has_local_image.png", rr)
	res, expires, err := up.OriginImageCheck(localPath, env)
	assert.Nil(t, err)
	assert.Nil(t, res)
	assert.Equal(t, expires, uint32(time.Now().Unix()+100)) // need to fix this and add delta
}

func Test_Upstream_SaveOriginImage_Success(t *testing.T) {
	up := testUpstream2()
	remotePath := "docs/favicon.png"
	metaPath, imagePath := up.LocalImagePath(remotePath, ".png", nil)

	res, expires, err := up.SaveOriginImage(remotePath, metaPath, imagePath, NewEnv(up))
	assert.Nil(t, err)
	assert.Nil(t, res)
	assert.Delta(t, expires, uint32(time.Now().Unix()+604800)-1, 2)

	assertFileHash(t, imagePath, "2c859096f003dddb6b78787eae13e910d3b268d374299645ae14063c689be8a4")
}

func Test_Upstream_SaveOriginImage_NotFound(t *testing.T) {
	up := testUpstream2()
	remotePath := "does_not_exist.png"
	metaPath, imagePath := up.LocalImagePath(remotePath, ".png", nil)

	res, expires, err := up.SaveOriginImage(remotePath, metaPath, imagePath, NewEnv(up))
	assert.Nil(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, expires, 0)

	// make sure the not found response is locally cached
	local := up.LoadLocalResponse(metaPath, nil, false)

	conn := &fasthttp.RequestCtx{}
	local.Write(conn, log.Noop{})
	body := request.Res(t, conn).ExpectNotFound().Body
	assert.StringContains(t, body, "404 Not Found")
}

func testUpstream2() *Upstream {
	return testUpstream("up2_local")
}

func testUpstream(name string) *Upstream {
	up, err := NewUpstream(name, &upstreamConfig{
		BaseURL: "https://www.goblgobl.com/",
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

func assertFileHash(t *testing.T, cachePath string, expected string) {
	t.Helper()

	hasher := sha256.New()
	content, err := os.ReadFile(cachePath)
	if err != nil {
		panic(err)
	}
	hasher.Write(content)
	actual := fmt.Sprintf("%x", hasher.Sum(nil))
	assert.Equal(t, actual, expected)
}

// This is a bit lame, but we modify our local file, so that we can
// assert that the file is being served from the local cache, and not
// being re-fetched from the upstream
func writeLocal(env *Env, p string, res Serializable) string {
	u := env.upstream
	localPath := u.LocalResPath(p, filepath.Ext(p))
	if err := u.save(res, localPath, env); err != nil {
		panic(err)
	}
	return localPath
}
