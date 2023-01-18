package assets

import (
	"bytes"
	gohttp "net/http"
	"testing"
	"time"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/utils/buffer"
)

func Test_Meta_Serialize_And_Read(t *testing.T) {
	m1 := &Meta{
		tpe:          9,
		status:       999,
		expires:      12,
		bodyLength:   345,
		contentType:  "a/type",
		cacheControl: "forever",
	}

	b := new(bytes.Buffer)
	assert.Nil(t, m1.Serialize(b))

	m2, err := MetaFromReader(testUpstream2(), b)
	assert.Nil(t, err)
	assert.Equal(t, m2.tpe, 9)
	assert.Equal(t, m2.status, 999)
	assert.Equal(t, m2.expires, 12)
	assert.Equal(t, m2.bodyLength, 345)
	assert.Equal(t, m2.contentType, "a/type")
	assert.Equal(t, m2.cacheControl, "forever")
}

func Test_Meta_FromResponse(t *testing.T) {
	res := &gohttp.Response{
		StatusCode: 800,
		Header: gohttp.Header{
			"Content-Type":  []string{"over/9000"},
			"Cache-Control": []string{"public,max-age=9001"},
		},
	}
	m := MetaFromResponse(res, 300, 100, 999)
	assert.Equal(t, m.tpe, 100)
	assert.Equal(t, m.status, 800)
	assert.Delta(t, m.expires, uint32(time.Now().Unix()+300), 1)
	assert.Equal(t, m.bodyLength, 999)
	assert.Equal(t, m.contentType, "over/9000")
	assert.Equal(t, m.cacheControl, "public,max-age=9001")
}

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
