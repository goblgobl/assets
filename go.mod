module src.goblgobl.com/assets

go 1.19

replace src.goblgobl.com/utils => ../utils

replace src.goblgobl.com/tests => ../tests

require (
	github.com/fasthttp/router v1.4.14
	github.com/valyala/fasthttp v1.43.0
	golang.org/x/sync v0.1.0
	src.goblgobl.com/tests v0.0.5
	src.goblgobl.com/utils v0.0.5
)

require (
	github.com/andybalholm/brotli v1.0.4 // indirect
	github.com/goccy/go-json v0.10.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/klauspost/compress v1.15.12 // indirect
	github.com/savsgio/gotils v0.0.0-20220530130905-52f3993e8d6d // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
)
