module src.goblgobl.com/assets

go 1.21

toolchain go1.21.1

//replace src.goblgobl.com/utils => ../utils
//replace src.goblgobl.com/tests => ../tests

require (
	github.com/fasthttp/router v1.4.20
	github.com/valyala/fasthttp v1.49.0
	golang.org/x/sync v0.1.0
	src.goblgobl.com/tests v0.0.8
	src.goblgobl.com/utils v0.0.8
)

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/klauspost/compress v1.16.3 // indirect
	github.com/savsgio/gotils v0.0.0-20230208104028-c358bd845dee // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
)
