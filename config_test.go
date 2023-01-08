package assets

import (
	"os/exec"
	"path"
	"strings"
	"testing"

	"src.goblgobl.com/tests/assert"
)

func Test_Config_InvalidPath(t *testing.T) {
	err := Configure("invalid.json")
	assert.Equal(t, err.Error(), "code: 103001 - open invalid.json: no such file or directory")
}

func Test_Config_InvalidJson(t *testing.T) {
	err := Configure(testConfigPath("invalid.json"))
	assert.Equal(t, err.Error(), "code: 103002 - expected colon after object key")
}

func Test_Config_Upstream_Base(t *testing.T) {
	err := Configure(testConfigPath("upstream_base.json"))
	assert.Equal(t, err.Error(), "code: 103004 - upstream must have a base_url")
}

func Test_Config_Minimal(t *testing.T) {
	err := Configure(testConfigPath("minimal.json"))
	assert.Nil(t, err)

	expectedPath, _ := exec.LookPath("vipsthumbnail")
	assert.True(t, expectedPath != "") // sanity

	assert.Equal(t, Config.VipsThumbnail, expectedPath)
	assert.True(t, strings.HasPrefix(Config.VipsVersion, "libvips "))

	assert.Equal(t, len(Config.Upstreams), 1)
	assert.Equal(t, Config.Upstreams["test"].BaseURL, "http://localhost:5400/x1")
}

func testConfigPath(file string) string {
	return path.Join("tests/configs/", file)
}
