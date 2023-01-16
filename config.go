package assets

import (
	"errors"
	"os"
	"os/exec"

	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/json"
	"src.goblgobl.com/utils/log"
)

var (
	Config         config
	DefaultCaching = []upstreamCacheConfig{
		upstreamCacheConfig{Status: 0, TTL: 300},
		upstreamCacheConfig{Status: 200, TTL: 3600},
	}
)

type config struct {
	InstanceId    uint8  `json:"instance_id"`
	VipsThumbnail string `json:"vipsthumbnail"`
	VipsVersion   string `json:"-"` // we set this ourselves

	CacheRoot string                     `json:"cache_root"`
	HTTP      httpConfig                 `json:"http"`
	Log       log.Config                 `json:"log"`
	Upstreams map[string]*upstreamConfig `json:"upstreams"`
}

type httpConfig struct {
	Listen string `json:"listen"`
}

type upstreamConfig struct {
	BaseURL        string                `json:"base_url"`
	Buffers        *buffer.Config        `json:"buffers"`
	Caching        []upstreamCacheConfig `json:"caching"`
	VipsTransforms map[string][]string   `json:"vips_transforms"`
}

type upstreamCacheConfig struct {
	Status int   `json:"status"`
	TTL    int32 `json:"ttl"`
}

func Configure(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return log.Err(ERR_CONFIG_READ, err)
	}

	if err := json.Unmarshal(data, &Config); err != nil {
		return log.Err(ERR_CONFIG_PARSE, err)
	}

	if err := log.Configure(Config.Log); err != nil {
		return err
	}

	if Config.CacheRoot == "" {
		Config.CacheRoot = "cache"
	}

	if Config.VipsThumbnail == "" {
		Config.VipsThumbnail, err = exec.LookPath("vipsthumbnail")
		if err != nil {
			return log.Err(ERR_CONFIG_VIPS_PATH, err)
		}
	}

	vipsVersion, err := exec.Command(Config.VipsThumbnail, "--vips-version").CombinedOutput()
	if err != nil {
		return log.Err(ERR_CONFIG_VIPS_VERSION, err)
	}
	Config.VipsVersion = string(vipsVersion)

	if len(Config.Upstreams) == 0 {
		return log.Err(ERR_CONFIG_ZERO_UPSTREAMS, errors.New("must have at least 1 upstream configured"))
	}

	for name, up := range Config.Upstreams {
		if up.BaseURL == "" {
			return log.Err(ERR_CONFIG_UPSTREAM_BASE, errors.New("upstream must have a base_url")).String("upstream", name)
		}

		if up.Buffers == nil {
			// we don't need particulalry large buffers, as all we're using
			// these for are generating cache keys and a few other string
			// concatenations
			up.Buffers = &buffer.Config{
				Count: 100,
				Min:   131072,  // 128KB,
				Max:   1048576, // 1MB
			}
		}

		if len(up.Caching) == 0 {
			up.Caching = DefaultCaching
		}
	}

	return nil
}
