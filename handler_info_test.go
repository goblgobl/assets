package assets

import (
	"runtime"
	"testing"

	"src.goblgobl.com/tests/assert"
	"src.goblgobl.com/tests/request"
	"src.goblgobl.com/utils/log"
)

func Test_InfoHandler_Ok(t *testing.T) {
	conn := request.Req(t).Conn()
	res, err := InfoHandler(conn)
	assert.Nil(t, err)

	res.Write(conn, log.Noop{})
	body := request.Res(t, conn).OK().JSON()
	assert.Equal(t, body.String("commit"), commit)
	assert.Equal(t, body.String("go"), runtime.Version())
}
