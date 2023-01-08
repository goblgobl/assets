package assets

import (
	_ "embed"
	"runtime"

	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils/http"
)

//go:generate make commit.txt
//go:embed commit.txt
var commit string

func InfoHandler(conn *fasthttp.RequestCtx) (http.Response, error) {
	return http.Ok(struct {
		Go     string `json:"go"`
		Commit string `json:"commit"`
	}{
		Commit: commit,
		Go:     runtime.Version(),
	}), nil
}
