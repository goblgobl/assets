package assets

import (
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
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
	up2 := testUpstream2()
	Upstreams = map[string]*Upstream{up2.name: up2}

	conn := request.Req(t).Query("up", up2.name).Conn()
	env, res, err := loadEnv(conn)
	assert.Nil(t, res)
	assert.Nil(t, err)

	assert.Equal(t, env.upstream.name, up2.name)
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
	assert.StringContains(t, res.Body, "openresty")

	// make sure we get this from the local file after our first fetch
	writeLocal(env, "not_exists", BuildRemoteResponse().Body("nope").Status(404).Response())
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
	writeLocal(env, "folder/main.js", BuildRemoteResponse().Body("hello").Status(200).Response())
	res = request.ReqT(t, env).
		UserValue("path", "folder/main.js").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Body, "hello")
}

func Test_ServerHandler_ExpiredLocal(t *testing.T) {
	env := NewEnv(testUpstream2())

	rr := BuildRemoteResponse().Body("hello").Status(200).Expires(-2).Response()
	localPath := writeLocal(env, "expired", rr)
	request.ReqT(t, env).
		UserValue("path", "expired").
		Get(AssetHandler).
		ExpectNotFound()

	// make sure it wrote the new version to our local cache
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
