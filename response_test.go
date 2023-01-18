package assets

import (
	"time"

	"src.goblgobl.com/utils/buffer"
)

type RemoteResponseBuilder struct {
	response *RemoteResponse
}

func BuildRemoteResponse() *RemoteResponseBuilder {
	return &RemoteResponseBuilder{
		response: &RemoteResponse{
			meta: &Meta{
				status:  200,
				expires: uint32(time.Now().Unix() + 100),
			},
			buffer: buffer.Empty,
		},
	}
}

func (rb *RemoteResponseBuilder) Response() *RemoteResponse {
	return rb.response
}

func (rb *RemoteResponseBuilder) Body(body string) *RemoteResponseBuilder {
	rb.response.buffer = buffer.Containing([]byte(body), 0)
	return rb
}

func (rb *RemoteResponseBuilder) Status(status int) *RemoteResponseBuilder {
	rb.response.meta.status = uint16(status)
	return rb
}

func (rb *RemoteResponseBuilder) ContentType(ct string) *RemoteResponseBuilder {
	rb.response.meta.contentType = ct
	return rb
}

func (rb *RemoteResponseBuilder) CacheControl(cc string) *RemoteResponseBuilder {
	rb.response.meta.cacheControl = cc
	return rb
}

func (rb *RemoteResponseBuilder) Expires(ttl int) *RemoteResponseBuilder {
	expires := time.Now().Unix() + int64(ttl)
	rb.response.meta.expires = uint32(expires)
	return rb
}

func (rb *RemoteResponseBuilder) Image() *RemoteResponseBuilder {
	rb.response.meta.tpe = TYPE_IMAGE
	return rb
}
