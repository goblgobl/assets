package assets

import (
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
)

func Test_PingHandler_Ok(t *testing.T) {
	conn := request.Req(t).Conn()
	res, err := PingHandler(conn)
	assert.Nil(t, err)

	res.Write(conn)
	body := request.Res(t, conn).OK()
	assert.Equal(t, body.Body, `{"ok":true}`)
}
