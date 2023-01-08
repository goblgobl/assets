package assets

import (
	"encoding/base64"

	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/http"
)

func ServeHandler(conn *fasthttp.RequestCtx, env *Env) (http.Response, error) {
	upstreamPath := conn.UserValue("path").(string)
	// extension := filepath.Ext(upstreamPath)

	// switch extension {
	// case ".png", ".jpg", ".gif":
	// 	return serveImage(conn, upstream, upstreamPath)
	// case ".woff2":
	// 	return serveStatic(conn, upstream, upstreamPath, false)
	// default:
	// 	return serveStatic(conn, upstream, upstreamPath, true)
	// }

	return serveStatic(conn, env, upstreamPath, true)
}

func serveStatic(conn *fasthttp.RequestCtx, env *Env, path string, compress bool) (http.Response, error) {
	upstream := env.upstream

	response, cachePath := upstream.LoadLocal(EncodePath(path), env)
	if response != nil {
		response.CacheHit()
		return response, nil
	}

	buf := upstream.buffers.Checkout()
	res, err := upstream.GetSaveAndServe(path, cachePath, buf, env)
	if err != nil {
		buf.Release()
		return nil, err
	}

	// buf will be released as part of the response lifecycle. It has
	// to exist as long as the response is needed since it holds the
	// data
	return res, nil
}

func EncodePath(path string) string {
	return base64.RawURLEncoding.EncodeToString(utils.S2B(path))
}