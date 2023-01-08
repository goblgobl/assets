package assets

import (
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/utils/buffer"
)

func init() {
	Config.CacheRoot = "tests/"
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
	res, path := u.LoadLocal(EncodePath("sample1.css"), nil)
	assert.Equal(t, path, "tests/up1_local/c2/c2FtcGxlMS5jc3M")
	assert.Equal(t, res.Status, 199)
	assert.Equal(t, res.ContentType, "assets/tests")
	assert.Equal(t, res.CacheControl, "private")
	assert.Equal(t, string(res.Body), "*{display:none}")

	res, path = u.LoadLocal(EncodePath("does_not_exist"), nil)
	assert.Equal(t, path, "tests/up1_local/ZG/ZG9lc19ub3RfZXhpc3Q")
	assert.Nil(t, res)
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
