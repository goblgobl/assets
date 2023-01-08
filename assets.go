package assets

import (
	"os"

	"src.goblgobl.com/utils/log"
)

var (
	Upstreams map[string]*Upstream
)

func Run() {
	Upstreams = make(map[string]*Upstream, len(Config.Upstreams))
	for name, config := range Config.Upstreams {
		upstream, err := NewUpstream(name, config)
		if err != nil {
			log.Fatal("new_upstream").Err(err).String("up", name).Log()
			os.Exit(1)
		}
		Upstreams[name] = upstream
	}
	Listen()
}
