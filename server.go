package assets

import (
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
	r.GET("/v1/{path:*}", http.Handler("v1", loadEnv, ServeHandler))

	// catch all
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		resNotFoundPath.Write(ctx)
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
