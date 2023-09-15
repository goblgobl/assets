package assets

import (
	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils/http"
	"src.goblgobl.com/utils/log"
)

type Env struct {
	requestId string

	upstream *Upstream

	// Anything logged with this logger will automatically have the rid
	// (request id) field
	logger log.Logger

	requestLogger log.Logger
}

func NewEnv(upstream *Upstream) *Env {
	requestId := upstream.NextRequestId()

	logger := log.Checkout().
		Field(upstream.logField).
		String("rid", requestId).
		MultiUse()

	return &Env{
		logger:    logger,
		upstream:  upstream,
		requestId: requestId,
	}
}

func (e *Env) RequestId() string {
	return e.requestId
}

func (e *Env) Info(ctx string) log.Logger {
	return e.logger.Info(ctx)
}

func (e *Env) Warn(ctx string) log.Logger {
	return e.logger.Warn(ctx)
}

func (e *Env) Error(ctx string) log.Logger {
	return e.logger.Error(ctx)
}

func (e *Env) Fatal(ctx string) log.Logger {
	return e.logger.Fatal(ctx)
}

func (e *Env) Request(route string) log.Logger {
	logger := log.Checkout().
		Field(e.upstream.logField).
		String("rid", e.requestId).
		Request(route)
	e.requestLogger = logger
	return logger
}

func (e *Env) Release() {
	e.logger.Release()
}

func (e *Env) ServerError(err error, conn *fasthttp.RequestCtx) http.Response {
	return http.ServerError(err, false)
}
