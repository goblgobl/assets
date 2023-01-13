package assets

import (
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/log"
)

var (
	errSingleflightLocalLoad = errors.New("Singleflight local load error")
)

type Upstream struct {
	name string

	client *gohttp.Client

	// Upstream-specific counter for generating the RequestId
	requestId uint32

	// the actual upstream server root (e.g. http://goblgobl.com/assets/)
	baseURL string

	// config.cache.root + upstream name
	cacheRoot string

	// thundering herd protection, limits infight upstream requests
	// to a single request, with any concurrent request waiting and
	// receiving the reply from the first
	sf *singleflight.Group

	buffers *buffer.Pool

	logField log.Field

	// status_code => ttl
	ttls map[int]int32

	// used when ttls[status_code] doesn't exist
	defaultTTL uint32
}

func NewUpstream(name string, config *upstreamConfig) (*Upstream, error) {
	cacheRoot := path.Join(Config.CacheRoot, name) + "/"
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		return nil, fmt.Errorf("Failed to make upstream cache root (%s) - %w", cacheRoot, err)
	}
	defaultTTL := int32(300)
	ttls := make(map[int]int32, len(config.Caching))
	for _, caching := range config.Caching {
		if caching.Status == 0 {
			defaultTTL = caching.TTL
		} else {
			ttls[caching.Status] = caching.TTL
		}
	}

	if defaultTTL < 0 {
		defaultTTL *= -1
	} else if defaultTTL == 0 {
		defaultTTL = 60
	}

	return &Upstream{
		sf:         new(singleflight.Group),
		baseURL:    config.BaseURL,
		client:     &gohttp.Client{},
		cacheRoot:  cacheRoot,
		defaultTTL: uint32(defaultTTL),
		ttls:       ttls,

		// If we let this start at 0, then restarts are likely to produce duplicates.
		// While we make no guarantees about the uniqueness of the requestId, there's
		// no reason we can't help things out a little.
		requestId: uint32(time.Now().Unix()),
		buffers:   buffer.NewPoolFromConfig(*config.Buffers),
		logField:  log.NewField().String("up", name).Finalize(),
	}, nil
}

func (u *Upstream) NextRequestId() string {
	nextId := atomic.AddUint32(&u.requestId, 1)
	return utils.EncodeRequestId(nextId, Config.InstanceId)
}

func (u *Upstream) IsFileCached(cacheKey string, env *Env) (string, bool) {
	cachePath := u.CachePath(cacheKey)
	_, err := os.Stat(cachePath)
	if err == nil {
		return cachePath, true
	}

	if os.IsNotExist(err) {
		return cachePath, false
	}

	env.Error("Upstream.IsFileCached").String("cacheKey", cacheKey).Err(err).Log()
	return cachePath, false
}

func (u *Upstream) LoadLocal(encodedPath string, env *Env, force bool) (*LocalResponse, string) {
	cachePath := u.CachePath(encodedPath)
	f, err := os.Open(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, cachePath
		}
		env.Error("Upstream.LoadLocal.open").String("path", cachePath).Err(err).Log()
		return nil, cachePath
	}

	res, err := NewLocalResponse(u, f)
	if err != nil {
		env.Error("Upstream.LoadLocal.read").String("path", cachePath).Err(err).Log()
		return nil, cachePath
	}

	// callers can opt to ignore the expiration
	if !force {
		expires := int(res.expires)
		if expires != 0 && int64(expires) < time.Now().Unix() {
			res.Close()
			// no need to delete this file, because we expect our caller to go fetch
			// it from the upstream and save the result, overwriting this
			return nil, cachePath
		}
	}

	return res, cachePath
}

func (u *Upstream) CachePath(cacheKey string) string {
	root := u.cacheRoot
	var b strings.Builder

	// +3 for the cacheKey[:2] + "/"
	b.Grow(len(root) + len(cacheKey) + 3)

	b.WriteString(root) // has / at the end
	b.WriteString(cacheKey[:2])
	b.WriteByte('/')
	b.WriteString(cacheKey)
	return b.String()
}

func (u *Upstream) GetSaveAndServe(remotePath string, local string, env *Env) (Response, error) {
	owner := false

	res, err, shared := u.sf.Do(remotePath, func() (any, error) {
		owner = true
		remoteURL := u.baseURL + remotePath
		res, err := u.client.Get(remoteURL)
		if err != nil {
			return nil, log.StructuredError{
				Err:  err,
				Code: ERR_PROXY,
				Data: map[string]any{"url": remoteURL},
			}
		}

		bodyReader := res.Body
		defer bodyReader.Close()

		buf := u.buffers.Checkout()
		_, err = io.Copy(buf, bodyReader)

		if err != nil {
			buf.Release()
			env.Error("Upstream.GetSaveAndServe.Copy").Err(err).Log()
			return nil, err
		}

		if err := buf.Error(); err != nil {
			buf.Release()
			env.Error("Upstream.GetSaveAndServe.Buffer").Err(err).Log()
			return nil, err
		}

		ttl := u.calculateTTL(res)
		response := NewRemoteResponse(res, buf, ttl)
		u.save(response, local, env)
		return response, nil
	})

	if err != nil {
		return nil, err
	}

	if !shared || owner {
		return res.(*RemoteResponse), nil
	}

	localRes, _ := u.LoadLocal(EncodePath(remotePath), env, true)
	if localRes == nil {
		env.Error("Upstream.GetSaveAndServe.LoadLocal").String("remote", remotePath).Log()
		return nil, errSingleflightLocalLoad
	}
	return localRes, nil
}

// We log the error here, because some cases won't care about this error
// and might just ignore it, but we still want to know about it
func (u *Upstream) save(response *RemoteResponse, local string, env *Env) error {
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	f, err := os.OpenFile(local, flag, 0600)
	if err != nil {
		os.MkdirAll(path.Dir(local), 0700)

		f, err = os.OpenFile(local, flag, 0600)
		if err != nil {
			env.Error("Upstream.Save.open").String("path", local).Err(err).Log()
			return err
		}
	}
	defer f.Close()
	if err := response.Serialize(f); err != nil {
		env.Error("Upstream.Save.serialize").Err(err).Log()
		return err
	}
	return nil
}

func (u *Upstream) calculateTTL(res *gohttp.Response) uint32 {
	status := res.StatusCode
	ttl, exists := u.ttls[status]

	if exists && ttl < 0 {
		// we have a configured TTL for this status code and it's set to be "forced"
		// (because of the negative value)
		return uint32(-ttl)
	}

	// At this point, we may not have a configured TTL for this status code
	// OR it could be configured to listen to the Cache-Control header if present
	// so we need to figure out the max-age from the response header
	cc := res.Header.Get("Cache-Control")

	// we possibly have a max-age, we should use that if it's valid
	if n := strings.Index(cc, "max-age="); n != -1 {
		if ttl := atoi(cc[n+8:]); ttl > 0 {
			return ttl
		}
	}

	// no max age, then either use the configured TTL for the status code OR
	// use the global
	if exists {
		return uint32(ttl)
	}
	return u.defaultTTL
}

// atoi that ignores any suffix (and overflow, but let's pretend we're ok with that)
func atoi(input string) uint32 {
	var n uint32
	for _, b := range []byte(input) {
		b -= '0'
		if b > 9 {
			break
		}
		n = n*10 + uint32(b)
	}
	return n
}
