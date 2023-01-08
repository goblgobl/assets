package assets

import (
	"net/http"

	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/log"
)

type Response struct {
	buffer       *buffer.Buffer
	upstream     log.Field
	cacheHit     bool
	Status       int
	ContentType  string
	CacheControl string
	RemotePath   string
	Body         []byte
}

func NewResponse(buf *buffer.Buffer, res *http.Response, remotePath string) *Response {
	h := res.Header
	body, _ := buf.Bytes()
	return &Response{
		buffer:       buf,
		Body:         body,
		Status:       res.StatusCode,
		RemotePath:   remotePath,
		ContentType:  h.Get("Content-Type"),
		CacheControl: h.Get("Cache-Control"),
	}
}

func (r *Response) Len() int {
	return len(r.Body)
}

func (r *Response) CacheHit() {
	r.cacheHit = true
}

func (r *Response) EnhanceLog(logger log.Logger) log.Logger {
	logger.String("remote", r.RemotePath).
		Int("res", r.Len()).
		Bool("cache", r.cacheHit).
		Int("status", r.Status)
	return logger
}

func (r *Response) Write(conn *fasthttp.RequestCtx) {
	conn.SetStatusCode(r.Status)

	header := &conn.Response.Header
	if ct := r.ContentType; ct != "" {
		header.SetContentType(ct)
	}
	if cc := r.CacheControl; cc != "" {
		header.SetBytesK([]byte("Cache-Control"), cc)
	}

	if buf := r.buffer; buf != nil {
		conn.SetBodyStream(buf, buf.Len())
	} else {
		conn.SetBody(r.Body)
	}
}
