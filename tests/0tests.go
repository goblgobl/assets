package tests

// This _needs_ to be called "0tests", because we need the init
// in this file to execute before the init in any other file
// (awful)

import (
	"math/rand"
	"time"

	"src.goblgobl.com/tests"
	"src.goblgobl.com/utils/log"
	"src.goblgobl.com/utils/validation"
)

var generator tests.Generator

func init() {
	rand.Seed(time.Now().UnixNano())

	err := log.Configure(log.Config{
		Level: "WARN",
	})
	if err != nil {
		panic(err)
	}

	err = validation.Configure(validation.Config{
		PoolSize:  1,
		MaxErrors: 10,
	})

	if err != nil {
		panic(err)
	}

}

func String(constraints ...int) string {
	return generator.String(constraints...)
}

func CaptureLog(fn func()) string {
	return tests.CaptureLog(fn)
}

func UUID() string {
	return generator.UUID()
}
