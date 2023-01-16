package assets

import (
	_ "embed"
	"os/exec"
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
	r.GET("/v1/ping", http.NoEnvHandler("ping", PingHandler))
	r.GET("/v1/info", http.NoEnvHandler("info", InfoHandler))

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
	extension := filepath.Ext(remotePath)

	switch extension {
	case ".png", ".jpg", ".gif", ".webp":
		return serveImage(conn, env, remotePath, extension)
	default:
		return serveStatic(conn, env, remotePath, extension)
	}
}

func serveImage(conn *fasthttp.RequestCtx, env *Env, remotePath string, extension string) (http.Response, error) {
	query := conn.QueryArgs()
	xform := query.Peek("xform")
	if xform == nil {
		// if we're not tranforming the image, we can treat it as a static asset
		return serveStatic(conn, env, remotePath, extension)
	}

	upstream := env.upstream
	xformArgs, exists := upstream.vipsTransforms[utils.B2S(xform)]
	if !exists {
		return resInvalidXForm, nil
	}

	// We have 2 layers of caching. The first is the transformed image, if we have
	// that, great, we can send it off as is. If we don't, then maybe we have the
	// origin image.

	localOriginPath := upstream.LocalPath(remotePath, extension)

	// TODO: optimize
	localXFormPath := localOriginPath + "_" + string(xform) + extension

	if res := upstream.LoadLocalImage(localXFormPath, env); res != nil {
		// We have the transformed image locally stored already, yay, send it
		return res, nil
	}

	res, exists, err := upstream.LocalImageCheck(localOriginPath, env)
	if res != nil || err != nil {
		// We have a response or an error, return that.
		// If we have a response, it's because we previously tried to load this image
		// and didn't get an image from the upstream, whatever we did get, we cached
		// and will now return to the client.
		return res, err
	}

	if !exists {
		// we don't have the origin, let's get it
		res, err := upstream.SaveOriginImage(remotePath, localOriginPath, env)
		if res != nil || err != nil {
			// We either got an error, or we got a non-image.
			// If we got a non-image, then we'll return the response as though
			// it's a static asset (this is likely a 404)
			return res, err
		}
	}

	//TODO: optimize this (absolutely necessary!)
	args := make([]string, len(xformArgs)+3)
	args[0] = localOriginPath
	args[1] = "-o"
	args[2] = localXFormPath
	for i := 0; i < len(xformArgs); i++ {
		args[i+3] = xformArgs[i]
	}

	cmd := exec.Command(Config.VipsThumbnail, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, log.StructuredError{
			Err:  err,
			Code: ERR_TRANSFORM,
			Data: map[string]any{
				"xform":  xform,
				"remote": remotePath,
				"stderr": string(out),
			},
		}
	}

	if res := upstream.LoadLocalImage(localXFormPath, env); res != nil {
		return res, nil
	}

	return nil, log.StructuredError{
		Err:  err,
		Code: ERR_TRANSFORM_MISSING,
		Data: map[string]any{
			"xform":  xform,
			"remote": remotePath,
			"local":  localXFormPath,
		},
	}
}

func serveStatic(conn *fasthttp.RequestCtx, env *Env, remotePath string, extension string) (http.Response, error) {
	upstream := env.upstream

	localPath := upstream.LocalPath(remotePath, extension)
	localRes := upstream.LoadLocalResponse(localPath, env, false)
	if localRes != nil {
		localRes.PathLog(remotePath)
		return localRes, nil
	}

	remoteRes, err := upstream.GetResponseAndSave(remotePath, localPath, env)
	if err != nil {
		return nil, err
	}
	remoteRes.PathLog(remotePath)
	return remoteRes, nil
}
