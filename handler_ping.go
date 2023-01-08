package assets

import (
	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils/http"
)

func PingHandler(conn *fasthttp.RequestCtx) (http.Response, error) {
	return http.OkBytes([]byte(`{"ok":true}`)), nil
}
