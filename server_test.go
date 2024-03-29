package assets

import (
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
	"src.goblgobl.com/utils/log"
)

const (
	UP2_ROOT = "cache/up2_local"
)

func init() {
	clearLocalCache()
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
	request.Res(t, conn).ExpectInvalid(202_002)
}

func Test_LoadEnv_Invalid_Up(t *testing.T) {
	conn := request.Req(t).Query("up", "nope").Conn()
	env, res, err := loadEnv(conn)
	assert.Nil(t, env)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	request.Res(t, conn).ExpectInvalid(202_003)
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

func Test_AssetHandler_NotFound(t *testing.T) {
	clearLocalCache()
	env := NewEnv(testUpstream2())

	request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(AssetHandler).
		ExpectNotFound(202_005)

		// load from cache
	request.ReqT(t, env).
		UserValue("path", "not_exists").
		Get(AssetHandler).
		ExpectNotFound(202_005)
}

func Test_AssetHandler_StaticAsset(t *testing.T) {
	env := NewEnv(testUpstream2())

	res := request.ReqT(t, env).
		UserValue("path", "assets/tests/main.css").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/css")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.Body, "*{display:none}\n")

	// again, should come from local file
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/main.css").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Headers["Content-Type"], "text/css")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.Body, "*{display:none}\n")

	// make sure we get this from the local file
	writeLocal(env, "assets/tests/main.css", BuildRemoteResponse().Body("hello").Status(200).Response())
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/main.css").
		Get(AssetHandler).
		OK()
	assert.Equal(t, res.Body, "hello")
}

func Test_AssetHandler_ExpiredLocal(t *testing.T) {
	env := NewEnv(testUpstream2())

	rr := BuildRemoteResponse().Body("hello").Status(200).Expires(-2).Response()
	writeLocal(env, "expired", rr)
	request.ReqT(t, env).
		UserValue("path", "expired").
		Get(AssetHandler).
		ExpectNotFound(202_005)
}

func Test_AssetHandler_InvalidXForm(t *testing.T) {
	env := NewEnv(testUpstream2())
	request.ReqT(t, env).
		UserValue("path", "nope.jpg").
		Query("xform", "invalid").
		Get(AssetHandler).
		ExpectInvalid(202_004)
}

func Test_AssetHandler_MissingOrigin_NoXForm(t *testing.T) {
	env := NewEnv(testUpstream2())
	request.ReqT(t, env).
		UserValue("path", "nope.jpg").
		Get(AssetHandler).
		ExpectNotFound()
}

func Test_AssetHandler_MissingOrigin_XForm(t *testing.T) {
	env := NewEnv(testUpstream2())
	request.ReqT(t, env).
		UserValue("path", "nope.jpg").
		Query("xform", "thumb_100").
		Get(AssetHandler).
		ExpectNotFound()
}

func Test_AssetHandler_Transform(t *testing.T) {
	clearLocalCache()
	env := NewEnv(testUpstream2())
	res := request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.png").
		Query("xform", "thumb_100").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 35291)
	assert.Equal(t, res.Headers["Content-Type"], "image/png")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "302a6235a7215e886c6ecc618bd0d741997b0d9c81cb8af15029d49ae8fcebb1")
	assertPNGDimensions(t, res.Bytes, 100, 150)

	// check cache path
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.png").
		Query("xform", "thumb_100").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 35291)
	assert.Equal(t, res.Headers["Content-Type"], "image/png")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "302a6235a7215e886c6ecc618bd0d741997b0d9c81cb8af15029d49ae8fcebb1")
	assertPNGDimensions(t, res.Bytes, 100, 150)

	// check differemt xform
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.png").
		Query("xform", "thumb_200").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 93329)
	assert.Equal(t, res.Headers["Content-Type"], "image/png")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "71eb3032b93c86d66e2c83911314467b32e04c4c3c78847452f917981bb2c639")
	assertPNGDimensions(t, res.Bytes, 200, 200)
}

func Test_AssetHandler_Orign(t *testing.T) {
	clearLocalCache()
	env := NewEnv(testUpstream2())
	res := request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.webp").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 77112)
	assert.Equal(t, res.Headers["Content-Type"], "image/webp")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "66dd82afbb49e776c32b27c51e0c51ed91c7f3ebe5f05aa2b575f29a96e52e99")

	// check cache path
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.webp").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 77112)
	assert.Equal(t, res.Headers["Content-Type"], "image/webp")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "66dd82afbb49e776c32b27c51e0c51ed91c7f3ebe5f05aa2b575f29a96e52e99")

	// xform with existing origin
	res = request.ReqT(t, env).
		UserValue("path", "assets/tests/tea.webp").
		Query("xform", "thumb_100").
		Get(AssetHandler).
		OK()

	assert.Equal(t, res.ContentLength, 6912)
	assert.Equal(t, res.Headers["Content-Type"], "image/webp")
	assertPublicCache(t, res.Headers["Cache-Control"], 598765)
	assert.Equal(t, res.SHA256(), "3d7d16e6a995fe2327b3d6090c1ea73ecc265edfea86b8ddfa655adbb363b063")
}

func clearLocalCache() {
	files, _ := ioutil.ReadDir(UP2_ROOT)
	for _, file := range files {
		os.RemoveAll(path.Join(UP2_ROOT, file.Name()))
	}
}

func assertPublicCache(t *testing.T, cc string, expected int) {
	t.Helper()
	assert.Equal(t, cc[:15], "public,max-age=")
	maxAge, _ := strconv.Atoi(cc[15:])
	assert.Delta(t, maxAge, expected, 2)
}

// https://www.openmymind.net/Getting-An-Images-Type-And-Size/
func assertPNGDimensions(t *testing.T, png []byte, expectedWith int, expectedHeight int) {
	t.Helper()

	// dimension info stats at byte 16
	png = png[16:]
	width := int(png[0])<<24 | int(png[1])<<16 | int(png[2])<<8 | int(png[3])
	height := int(png[4])<<24 | int(png[5])<<16 | int(png[6])<<8 | int(png[7])

	assert.Equal(t, width, expectedWith)
	assert.Equal(t, height, expectedHeight)
}
