package assets

import (
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/log"
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

func Test_InfoHandler_Ok(t *testing.T) {
	conn := request.Req(t).Conn()
	res, err := InfoHandler(conn)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	body := request.Res(t, conn).OK().JSON()
	assert.Equal(t, body.String("commit"), commit)
	assert.Equal(t, body.String("go"), runtime.Version())
}

func Test_PingHandler_Ok(t *testing.T) {
	conn := request.Req(t).Conn()
	res, err := PingHandler(conn)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	body := request.Res(t, conn).OK()
	assert.Equal(t, body.Body, `{"ok":true}`)
}

func Test_LoadEnv_Missing_Up(t *testing.T) {
	conn := request.Req(t).Conn()
	env, res, err := loadEnv(conn)
	assert.Nil(t, env)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	request.Res(t, conn).ExpectInvalid(102_002)
}

func Test_LoadEnv_Invalid_Up(t *testing.T) {
	conn := request.Req(t).Query("up", "nope").Conn()
	env, res, err := loadEnv(conn)
	assert.Nil(t, env)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	request.Res(t, conn).ExpectInvalid(102_003)
}

func Test_LoadEnv_Ok(t *testing.T) {
	up1 := testUpstream1()
	Upstreams = map[string]*Upstream{up1.name: testUpstream1()}

	conn := request.Req(t).Query("up", up1.name).Conn()
	env, res, err := loadEnv(conn)
	assert.Nil(t, res)
	assert.Nil(t, err)

	assert.Equal(t, env.upstream.name, up1.name)
}

func init() {
	files, _ := ioutil.ReadDir(UP2_ROOT)
	for _, file := range files {
		os.RemoveAll(path.Join(UP2_ROOT, file.Name()))
	}
}

func Test_AssetHandler_NotFound(t *testing.T) {
	env := NewEnv(testUpstream2())

	res := request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(AssetHandler).
		ExpectNotFound()
	assert.True(t, strings.HasSuffix(res.Body, "</html>\r\n"))

	// make sure we get this from the local file after our first fetch
	writeLocalResponse(env, "not_exists", &RemoteResponse{buffer: buffer.Containing([]byte("nope"), 0), status: 404})
	res = request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(AssetHandler).
		ExpectNotFound()
	assert.Equal(t, res.Body, "nope")
}

func Test_AssetHandler_StaticAsset(t *testing.T) {
	env := NewEnv(testUpstream2())

	res := request.ReqT(t, env).
		UserValue("path", "assets/folder/main.js").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/javascript")
	assert.Equal(t, res.Headers["Cache-Control"], "public, max-age=604800")
	assert.Equal(t, res.Body, "alert(\"hi\")\n")

	// again, should come from local file
	res = request.ReqT(t, env).
		UserValue("path", "assets/folder/main.js").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/javascript")
	assert.Equal(t, res.Headers["Cache-Control"], "public, max-age=604800")
	assert.Equal(t, res.Body, "alert(\"hi\")\n")

	// make sure we get this from the local file
	writeLocalResponse(env, "folder/main.js", &RemoteResponse{buffer: buffer.Containing([]byte("hello"), 0), status: 200})
	res = request.ReqT(t, env).
		UserValue("path", "folder/main.js").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Body, "hello")
}

func Test_ServerHandler_ExpiredLocal(t *testing.T) {
	env := NewEnv(testUpstream2())

	now := time.Now().Unix()
	writeLocalResponse(env, "expired", &RemoteResponse{buffer: buffer.Containing([]byte("hello"), 0), status: 200, expires: uint32(now - 2)})
	request.ReqT(t, env).
		UserValue("path", "expired").
		Get(AssetHandler).
		ExpectNotFound()

	// make sure it wrote the new version to our local cache
	localPath := env.upstream.LocalPath("expired", "")
	data, _ := os.ReadFile(localPath)
	assert.StringContains(t, string(data), "404 Not Found")
}

func Test_ServerHandler_InvalidXForm(t *testing.T) {
	env := NewEnv(testUpstream2())
	request.ReqT(t, env).
		UserValue("path", "nope.jpg").
		Query("xform", "invalid").
		Get(AssetHandler).
		ExpectInvalid(102_004)
}
