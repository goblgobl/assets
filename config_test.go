package assets

import (
	"os/exec"
	"path"
	"strings"
	"testing"

	"src.goblgobl.com/tests/assert"
)

var testConfig config

func init() {
	if err := Configure(testConfigPath("minimal.json")); err != nil {
		panic(err)
	}
	testConfig = Config
}

func Test_Config_InvalidPath(t *testing.T) {
	defer func() { Config = testConfig }()
	err := Configure("invalid.json")
	assert.Equal(t, err.Error(), "code: 103001 - open invalid.json: no such file or directory")
}

func Test_Config_InvalidJson(t *testing.T) {
	defer func() { Config = testConfig }()
	err := Configure(testConfigPath("invalid.json"))
	assert.Equal(t, err.Error(), "code: 103002 - expected colon after object key")
}

func Test_Config_Upstream_Base(t *testing.T) {
	defer func() { Config = testConfig }()
	err := Configure(testConfigPath("upstream_base.json"))
	assert.Equal(t, err.Error(), "code: 103004 - upstream must have a base_url")
}

func Test_Config_Minimal(t *testing.T) {
	defer func() { Config = testConfig }()
	err := Configure(testConfigPath("minimal.json"))
	assert.Nil(t, err)

	expectedPath, _ := exec.LookPath("vipsthumbnail")
	assert.True(t, expectedPath != "") // sanity

	assert.Equal(t, Config.VipsThumbnail, expectedPath)
	assert.True(t, strings.HasPrefix(Config.VipsVersion, "libvips "))

	assert.Equal(t, len(Config.Upstreams), 1)
	up1 := Config.Upstreams["test"]
	assert.Equal(t, up1.BaseURL, "http://localhost:5400/x1")

	assert.Equal(t, len(up1.Caching), 2)
	assert.Equal(t, up1.Caching[0].Status, 0)
	assert.Equal(t, up1.Caching[0].TTL, 300)
	assert.Equal(t, up1.Caching[1].Status, 200)
	assert.Equal(t, up1.Caching[1].TTL, 3600)
}

func testConfigPath(file string) string {
	return path.Join("tests/configs/", file)
}
