package main

import (
	"flag"

	"src.goblgobl.com/assets"
	"src.goblgobl.com/utils/log"
)

func main() {
	configPath := flag.String("config", "config.json", "full path to config file")
	flag.Parse()

	err := assets.Configure(*configPath)
	if err != nil {
		log.Fatal("load_config").String("path", *configPath).Err(err).Log()
		return
	}

	assets.Run()
}
