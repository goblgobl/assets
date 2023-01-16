package assets

import (
	"encoding/binary"
	"errors"
	"io"
	gohttp "net/http"
	"os"
	"time"

	"github.com/valyala/fasthttp"
	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/http"
	"src.goblgobl.com/utils/log"
)

var (
	BIN_ENCODER                    = binary.LittleEndian
	ErrInvalidResponseHeaderLength = errors.New("Serialized response header is invalid")
	ErrInvalidResponseType         = errors.New("Serialized response is an unknown type")
	ErrInvalidResponseVersion      = errors.New("Serialized response is an unsuported version")
)

type Response interface {
	http.Response
	PathLog(string)
}

// A response that's based on an net/http.Response from a GET to the upstream.
// This is like a ProxyResponse, except it's designed not only to proxy the
// request, but also serialize itself to disk (acting as a cache that can
// then be loaded as a LocalResponse)
type RemoteResponse struct {
	path         string
	buffer       *buffer.Buffer
	status       int
	contentType  string
	cacheControl string
	expires      uint32
}

func NewRemoteResponse(res *gohttp.Response, buf *buffer.Buffer, ttl uint32) *RemoteResponse {
	h := res.Header
	expires := uint32(time.Now().Add(time.Duration(ttl) * time.Second).Unix())
	return &RemoteResponse{
		buffer:       buf,
		expires:      expires,
		status:       res.StatusCode,
		contentType:  h.Get("Content-Type"),
		cacheControl: h.Get("Cache-Control"),
	}
}

func (r *RemoteResponse) PathLog(path string) {
	r.path = path
}

func (r *RemoteResponse) Write(conn *fasthttp.RequestCtx, logger log.Logger) log.Logger {
	status := r.status
	bodyLength := r.buffer.Len()

	conn.SetStatusCode(status)

	header := &conn.Response.Header
	if ct := r.contentType; ct != "" {
		header.SetContentType(ct)
	}
	if cc := r.cacheControl; cc != "" {
		header.SetBytesK([]byte("Cache-Control"), cc)
	}

	conn.SetBodyStream(r, bodyLength)

	return logger.
		Bool("hit", false).
		String("path", r.path).
		Int("res", bodyLength).
		Int("status", status)
}

// Close will automatically be called when the response is written (this is
// handled by fasthttp). But, if ever we discard a RemoteResponse, or use
// it outside of fasthttp, we need to call Close
func (r *RemoteResponse) Close() error {
	return r.buffer.Close()
}

func (r *RemoteResponse) Read(p []byte) (int, error) {
	return r.buffer.Read(p)
}

func (r *RemoteResponse) Serialize(w io.Writer) error {
	var header [16]byte
	body, _ := r.buffer.Bytes()

	// magic number so we can tell this type of response apart from a raw image
	header[0] = 1
	header[1] = 1

	// version
	// header[2] = 0
	header[3] = 1

	BIN_ENCODER.PutUint32(header[4:], r.expires)
	BIN_ENCODER.PutUint16(header[8:], uint16(r.status))
	header[10] = byte(len(r.contentType))
	header[11] = byte(len(r.cacheControl))
	BIN_ENCODER.PutUint32(header[12:], uint32(len(body)))

	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	if _, err := w.Write(utils.S2B(r.contentType)); err != nil {
		return err
	}
	if _, err := w.Write(utils.S2B(r.cacheControl)); err != nil {
		return err
	}

	_, err := w.Write(body)
	return err
}

// A response which is loaded from the local file system. Or a "cached" response
// We expect most responses to be a LocalResponse, because we expect heavy caching.
type LocalResponse struct {
	path     string
	expires  uint32
	header   [16]byte
	file     *os.File
	upstream *Upstream
}

func NewLocalResponse(upstream *Upstream, file *os.File) (*LocalResponse, error) {
	var header [16]byte
	n, err := file.Read(header[:])
	if err != nil {
		return nil, err
	}
	if n != 16 {
		return nil, ErrInvalidResponseHeaderLength
	}

	if header[0] != 1 || header[1] != 1 {
		return nil, ErrInvalidResponseType
	}

	if header[2] != 0 || header[3] != 1 {
		return nil, ErrInvalidResponseVersion
	}

	return &LocalResponse{
		file:     file,
		header:   header,
		upstream: upstream,
		expires:  BIN_ENCODER.Uint32(header[4:]),
	}, nil
}

func (r *LocalResponse) PathLog(path string) {
	r.path = path
}

func (r *LocalResponse) Write(conn *fasthttp.RequestCtx, logger log.Logger) log.Logger {
	file := r.file
	header := r.header

	// we're already read the magic header (bytes 0, 1), version (bytes 2, 3)
	// and expiry (bytes 4, 5, 6, 7)

	status := int(BIN_ENCODER.Uint16(header[8:]))
	contentTypeLength := header[10]
	cacheControlLength := header[11]
	bodyLength := int(BIN_ENCODER.Uint32(header[12:]))

	conn.SetStatusCode(status)

	if contentTypeLength > 0 || cacheControlLength > 0 {
		buffer := r.upstream.buffers.Checkout()
		defer buffer.Release()
		// this should not be able to fail, since our config enforces buffers are
		// configured with at least 255 bytes
		scrap, _ := buffer.TakeBytes(255)

		header := &conn.Response.Header

		if contentTypeLength > 0 {
			ct := scrap[:contentTypeLength]
			if n, _ := file.Read(ct); n > 0 {
				header.SetContentTypeBytes(ct)
			}
		}
		if cacheControlLength > 0 {
			cc := scrap[:cacheControlLength]
			if n, _ := file.Read(cc); n > 0 {
				header.SetBytesKV([]byte("Cache-Control"), cc)
			}
		}
	}

	// SetBodyStream will close the file
	conn.SetBodyStream(r, bodyLength)

	return logger.
		Bool("hit", true).
		String("path", r.path).
		Int("res", bodyLength).
		Int("status", status)
}

// Close will automatically be called when the response is written (this is
// handled by fasthttp), but there might be cases where a LocalResponse is never
// writen (like if we decide that it's invalid, such as when it's expired
func (r *LocalResponse) Close() error {
	return r.file.Close()
}

func (r *LocalResponse) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

// A response of an image off the file system
type ImageResponse struct {
	file *os.File
}

func NewImageResponse(file *os.File) ImageResponse {
	return ImageResponse{file: file}
}

// func (r *ImageResponse) PathLog(path string) {
// 	r.path = path
// }

func (r ImageResponse) Write(conn *fasthttp.RequestCtx, logger log.Logger) log.Logger {
	stats, _ := r.file.Stat()
	bodyLength := int(stats.Size())

	conn.SetStatusCode(200)
	// TODO: content type

	// SetBodyStream will close the file
	conn.SetBodyStream(r, bodyLength)

	return logger.
		Bool("hit", true).
		// String("path", r.path).
		Int("res", bodyLength).
		Int("status", 200)
}

// Close will automatically be called when the response is written (this is
// handled by fasthttp), but there might be cases where a LocalResponse is never
// writen (like if we decide that it's invalid, such as when it's expired
func (r ImageResponse) Close() error {
	return r.file.Close()
}

func (r ImageResponse) Read(p []byte) (int, error) {
	return r.file.Read(p)
}
