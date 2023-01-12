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

	localRes, cachePath := upstream.LoadLocal(EncodePath(path), env, false)
	if localRes != nil {
		localRes.Path(path)
		return localRes, nil
	}

	remoteRes, err := upstream.GetSaveAndServe(path, cachePath, env)
	if err != nil {
		return nil, err
	}
	remoteRes.Path(path)
	return remoteRes, nil
}

func EncodePath(path string) string {
	return base64.RawURLEncoding.EncodeToString(utils.S2B(path))
}
