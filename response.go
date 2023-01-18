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
	"src.goblgobl.com/utils/log"
)

const (
	TYPE_GENERIC byte = 0
	TYPE_IMAGE        = 1
)

var (
	ErrInvalidResponseHeaderLength = errors.New("serialized response header is invalid")
	ErrInvalidResponseType         = errors.New("serialized response is an unknown type")
	ErrInvalidResponseVersion      = errors.New("serialized response is an unsuported version")
	BIN_ENCODER                    = binary.LittleEndian
)

type Serializable interface {
	Serialize(w io.Writer) error
}

type Meta struct {
	tpe          byte
	status       uint16
	contentType  string
	cacheControl string
	expires      uint32
	bodyLength   uint32
}

// Don't rely on res.ContentLength for the bodyLength, it isn't reliable.
// (It can be unknown (aka 0) to the gohttp.Response). Let our caller
// give us the length explicitly (it probably read the body)
func MetaFromResponse(res *gohttp.Response, ttl uint32, tpe byte, bodyLength uint32) *Meta {
	h := res.Header
	expires := uint32(time.Now().Add(time.Duration(ttl) * time.Second).Unix())
	return &Meta{
		tpe:          tpe,
		expires:      expires,
		bodyLength:   bodyLength,
		status:       uint16(res.StatusCode),
		contentType:  h.Get("Content-Type"),
		cacheControl: h.Get("Cache-Control"),
	}
}

func MetaFromReader(upstream *Upstream, r io.Reader) (*Meta, error) {
	var header [17]byte
	n, err := r.Read(header[:])
	if err != nil {
		return nil, err
	}

	if n != 17 {
		return nil, ErrInvalidResponseHeaderLength
	}

	if header[0] != 1 || header[1] != 1 {
		return nil, ErrInvalidResponseType
	}

	if header[2] != 0 || header[3] != 1 {
		return nil, ErrInvalidResponseVersion
	}

	contentTypeLength := header[11]
	cacheControlLength := header[12]
	var contentType, cacheControl string

	if contentTypeLength > 0 || cacheControlLength > 0 {
		buffer := upstream.buffers.Checkout()
		defer buffer.Release()
		// this should not be able to fail, since our config enforces buffers are
		// configured with at least 255 bytes
		scrap, _ := buffer.TakeBytes(255)

		if contentTypeLength > 0 {
			ct := scrap[:contentTypeLength]
			if n, _ := r.Read(ct); n > 0 {
				contentType = string(ct)
			}
		}
		if cacheControlLength > 0 {
			cc := scrap[:cacheControlLength]
			if n, _ := r.Read(cc); n > 0 {
				cacheControl = string(cc)
			}
		}
	}

	return &Meta{
		tpe:          header[4],
		expires:      BIN_ENCODER.Uint32(header[5:]),
		status:       BIN_ENCODER.Uint16(header[9:]),
		contentType:  contentType,
		cacheControl: cacheControl,
		bodyLength:   BIN_ENCODER.Uint32(header[13:]),
	}, nil
}

func (m *Meta) Serialize(w io.Writer) error {
	var header [17]byte

	// magic number so we can tell this type of response apart from a raw image
	header[0] = 1
	header[1] = 1

	// version
	// header[2] = 0
	header[3] = 1
	header[4] = m.tpe

	BIN_ENCODER.PutUint32(header[5:], m.expires)
	BIN_ENCODER.PutUint16(header[9:], m.status)
	header[11] = byte(len(m.contentType))
	header[12] = byte(len(m.cacheControl))
	BIN_ENCODER.PutUint32(header[13:], m.bodyLength)

	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	if _, err := w.Write(utils.S2B(m.contentType)); err != nil {
		return err
	}
	if _, err := w.Write(utils.S2B(m.cacheControl)); err != nil {
		return err
	}
	return nil
}

// A response that's based on an net/http.Response from a GET to the upstream.
// This is like a ProxyResponse, except it's designed not only to proxy the
// request, but also serialize itself to disk (acting as a cache that can
// then be loaded as a LocalResponse)
type RemoteResponse struct {
	meta   *Meta
	buffer *buffer.Buffer
}

func NewRemoteResponse(res *gohttp.Response, buf *buffer.Buffer, ttl uint32, tpe byte) *RemoteResponse {
	return &RemoteResponse{
		buffer: buf,
		meta:   MetaFromResponse(res, ttl, tpe, uint32(buf.Len())),
	}
}

func (r *RemoteResponse) Write(conn *fasthttp.RequestCtx, logger log.Logger) log.Logger {
	meta := r.meta
	status := int(meta.status)
	bodyLength := int(meta.bodyLength)

	conn.SetStatusCode(status)

	header := &conn.Response.Header
	if ct := meta.contentType; ct != "" {
		header.SetContentType(ct)
	}
	if cc := meta.cacheControl; cc != "" {
		header.SetBytesK([]byte("Cache-Control"), cc)
	}

	conn.SetBodyStream(r, bodyLength)

	return logger.
		Bool("hit", false).
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
	if err := r.meta.Serialize(w); err != nil {
		return err
	}
	body, _ := r.buffer.Bytes()
	_, err := w.Write(body)
	return err
}

// A response which is loaded from the local file system. Or a "cached" response
// We expect most responses to be a LocalResponse, because we expect heavy caching.
type LocalResponse struct {
	hit      bool
	meta     *Meta
	file     *os.File
	upstream *Upstream
}

func NewLocalResponse(upstream *Upstream, file *os.File) (*LocalResponse, error) {
	meta, err := MetaFromReader(upstream, file)
	if err != nil {
		return nil, err
	}

	return &LocalResponse{
		hit:      true,
		file:     file,
		meta:     meta,
		upstream: upstream,
	}, nil
}

func (r *LocalResponse) Type() byte {
	return r.meta.tpe
}

func (r *LocalResponse) Write(conn *fasthttp.RequestCtx, logger log.Logger) log.Logger {
	meta := r.meta
	status := int(meta.status)
	bodyLength := int(meta.bodyLength)

	conn.SetStatusCode(status)
	header := &conn.Response.Header
	if ct := meta.contentType; ct != "" {
		header.SetContentType(ct)
	}
	if cc := meta.cacheControl; cc != "" {
		header.SetBytesK([]byte("Cache-Control"), cc)
	}

	// SetBodyStream will close the file
	conn.SetBodyStream(r, bodyLength)

	return logger.
		Bool("hit", r.hit).
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
