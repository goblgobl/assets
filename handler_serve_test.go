package assets

import (
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
)

const (
	UP2_ROOT = "tests/up2_local"
)

func init() {
	files, _ := ioutil.ReadDir(UP2_ROOT)
	for _, file := range files {
		os.RemoveAll(path.Join(UP2_ROOT, file.Name()))
	}
}

func Test_EncodePath(t *testing.T) {
	assert.Equal(t, EncodePath("Abc-32?1/32"), "QWJjLTMyPzEvMzI")
}

func Test_ServeHandler_NotFound(t *testing.T) {
	u := testUpstream2()
	env := NewEnv(u)

	res := request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(ServeHandler).
		ExpectNotFound()
	assert.True(t, strings.HasSuffix(res.Body, "</html>\r\n"))

	// make sure we get this from the local file after our first fetch
	wirteLocal(env, "not_exists", &Response{Body: []byte("nope"), Status: 404})
	res = request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(ServeHandler).
		ExpectNotFound()
	assert.Equal(t, res.Body, "nope")
}

func Test_ServeHandler_StaticAsset(t *testing.T) {
	u := testUpstream2()
	env := NewEnv(u)

	res := request.ReqT(t, env).
		UserValue("path", "folder/main.js").
		Get(ServeHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/javascript")
	assert.Equal(t, res.Headers["Cache-Control"], "public, max-age=604800")
	assert.Equal(t, res.Body, "alert(\"hi\")\n")

	// again, should come from local file
	res = request.ReqT(t, env).
		UserValue("path", "folder/main.js").
		Get(ServeHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/javascript")
	assert.Equal(t, res.Headers["Cache-Control"], "public, max-age=604800")
	assert.Equal(t, res.Body, "alert(\"hi\")\n")

	// make sure we get this from the local file
	wirteLocal(env, "folder/main.js", &Response{Body: []byte("hello"), Status: 200})
	res = request.ReqT(t, env).
		UserValue("path", "folder/main.js").
		Get(ServeHandler).
		OK()
	assert.Equal(t, res.Body, "hello")
}

// This is a bit lame, but we modify our local file, so that we can
// assert that the file is being served from the local cache, and not
// being re-fetched from the upstream
func wirteLocal(env *Env, p string, res *Response) {
	u := env.upstream
	p = EncodePath(p)
	if err := u.save(res, u.CachePath(p), env); err != nil {
		panic(err)
	}
}
