package assets

import (
	_ "embed"
	"path/filepath"
	"runtime"

	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/http"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils/log"
)

var (
	resNotFoundPath   = http.StaticNotFound(RES_UNKNOWN_ROUTE)
	resMissingUpParam = http.StaticError(400, RES_MISSING_UP_PARAM, "up parameter is required")
	resUnknownUpParam = http.StaticError(400, RES_UNKNOWN_UP_PARAM, "up parameter is not valid")
	resInvalidXForm   = http.StaticError(400, RES_INVALID_XFORM_PARAM, "invalid xform parameter")
	//go:generate make commit.txt
	//go:embed commit.txt
	commit string
)

func Listen() {
	listen := Config.HTTP.Listen
	if listen == "" {
		listen = "127.0.0.1:5300"
	}

	log.Info("server_listening").String("address", listen).Log()

	fast := fasthttp.Server{
		Handler:                      handler(),
		NoDefaultContentType:         true,
		NoDefaultServerHeader:        true,
		SecureErrorLogMessage:        true,
		DisablePreParseMultipartForm: true,
	}
	err := fast.ListenAndServe(listen)
	log.Fatal("http_server_error").Err(err).String("address", listen).Log()
}

func handler() func(ctx *fasthttp.RequestCtx) {
	r := router.New()

	// misc routes
	r.GET("/ping", http.NoEnvHandler("ping", PingHandler))
	r.GET("/info", http.NoEnvHandler("info", InfoHandler))

	// asset proxy routes
	r.GET("/v1/{path:*}", http.Handler("v1", loadEnv, AssetHandler))

	// catch all
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		resNotFoundPath.Write(ctx, log.Request("404"))
	}

	return r.Handler
}

func loadEnv(conn *fasthttp.RequestCtx) (*Env, http.Response, error) {
	query := conn.QueryArgs()

	up := query.Peek("up")
	if up == nil {
		return nil, resMissingUpParam, nil
	}

	upstream, ok := Upstreams[utils.B2S(up)]
	if !ok {
		return nil, resUnknownUpParam, nil
	}
	return NewEnv(upstream), nil, nil
}

func InfoHandler(conn *fasthttp.RequestCtx) (http.Response, error) {
	return http.Ok(struct {
		Go     string `json:"go"`
		Commit string `json:"commit"`
	}{
		Commit: commit,
		Go:     runtime.Version(),
	}), nil
}

func PingHandler(conn *fasthttp.RequestCtx) (http.Response, error) {
	return http.OkBytes([]byte(`{"ok":true}`)), nil
}

func AssetHandler(conn *fasthttp.RequestCtx, env *Env) (http.Response, error) {
	remotePath := conn.UserValue("path").(string)
	env.requestLogger.String("path", remotePath)

	extension := lowercase(filepath.Ext(remotePath))
	switch extension {
	case ".png", ".jpg", ".gif", ".webp":
		return serveImage(conn, env, remotePath, extension)
	default:
		return serveStatic(conn, env, remotePath, extension)
	}
}

func serveImage(conn *fasthttp.RequestCtx, env *Env, remotePath string, extension string) (http.Response, error) {
	upstream := env.upstream

	query := conn.QueryArgs()
	xform := query.Peek("xform")

	var xformArgs []string
	if xform != nil {
		if xformArgs = upstream.transforms[utils.B2S(xform)]; xformArgs == nil {
			return resInvalidXForm, nil
		}
	}

	localMetaPath, localImagePath := upstream.LocalImagePath(remotePath, extension, xform)

	if res := upstream.LoadLocalImage(localMetaPath, localImagePath, env); res != nil {
		// We have a local response for this request. Hopefully it's the image
		// that was asked for, but it could be anything else that the upstream
		// returned previously that we've now cached (e.g. a 404)
		return res, nil
	}

	if xform == nil {
		// no tranform and from the previous failed LoadLocalImage, we know we
		// don't have the image.
		res, _, err := upstream.SaveOriginImage(remotePath, localMetaPath, localImagePath, env)
		if res != nil || err != nil {
			// As an optimization, SaveOriginImage will return the RemoteResponse or
			// LocalResponse if the upstream returned a non-image, we can return that
			// as is
			return res, err
		}
		if res := upstream.LoadLocalImage(localMetaPath, localImagePath, env); res != nil {
			// not considered a cache hit since we had to fetch it from the origin first
			res.hit = false
			return res, nil
		}

		return nil, log.ErrData(ERR_LOCAL_IMAGE_MISSING, err, map[string]any{
			"remote": remotePath,
			"local":  localImagePath,
		})
	}

	originMetaPath, originImagePath := upstream.LocalImagePath(remotePath, extension, nil)
	res, expires, err := upstream.OriginImageCheck(originMetaPath, env)
	if res != nil || err != nil {
		// We have a response or an error, return that.
		// If we have a response, it's because we previously tried to load the origin
		// and didn't get an image from the upstream, whatever we did get, we cached
		// and will now return to the client.
		return res, err
	}

	if expires == 0 {
		// we don't have the origin, let's get it
		res, ex, err := upstream.SaveOriginImage(remotePath, originMetaPath, originImagePath, env)
		if res != nil || err != nil {
			// We either got an error, or we got a non-image.
			// If we got a non-image, then we'll return the response as though
			// it's a static asset (this is likely a 404)

			// TODO: do we want to store the origin res as a transform res?
			return res, err
		}
		expires = ex
	}

	if err := upstream.TransformImage(originImagePath, localMetaPath, localImagePath, xformArgs, expires, env); err != nil {
		return nil, log.ErrData(ERR_TRANSFORM, err, map[string]any{
			"xform":  xform,
			"remote": remotePath,
		})
	}

	if res := upstream.LoadLocalImage(localMetaPath, localImagePath, env); res != nil {
		// not considered a cache hit since we had to fetch it from the origin first
		res.hit = false
		return res, nil
	}

	return nil, log.ErrData(ERR_LOCAL_IMAGE_MISSING, err, map[string]any{
		"remote": remotePath,
		"local":  localImagePath,
	})
}

func serveStatic(conn *fasthttp.RequestCtx, env *Env, remotePath string, extension string) (http.Response, error) {
	upstream := env.upstream

	localPath := upstream.LocalResPath(remotePath, extension)
	lr := upstream.LoadLocalResponse(localPath, env, false)
	if lr != nil {
		return lr, nil
	}

	return upstream.GetResponseAndSave(remotePath, localPath, env)
}
