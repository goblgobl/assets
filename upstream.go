package assets

import (
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
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

type Upstream struct {
	client *http.Client

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
}

func NewUpstream(name string, config *upstreamConfig) (*Upstream, error) {
	cacheRoot := path.Join(Config.CacheRoot, name) + "/"
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		return nil, fmt.Errorf("Failed to make upstream cache root (%s) - %w", cacheRoot, err)
	}

	return &Upstream{
		baseURL:   config.BaseURL,
		client:    &http.Client{},
		cacheRoot: cacheRoot,
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

func (u *Upstream) LoadLocal(encodedPath string, env *Env) (*Response, string) {
	cachePath := u.CachePath(encodedPath)
	f, err := os.Open(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, cachePath
		}
		env.Error("Upstream.LoadLocal").String("path", cachePath).Err(err).Log()
		return nil, cachePath
	}
	defer f.Close()

	var response *Response
	if err := gob.NewDecoder(f).Decode(&response); err != nil {
		env.Error("Upstream.LoadLocal.decode").String("path", cachePath).Err(err).Log()
		return nil, cachePath
	}
	return response, cachePath
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

func (u *Upstream) GetSaveAndServe(remotePath string, local string, buf *buffer.Buffer, env *Env) (*Response, error) {
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
	_, err = io.Copy(buf, bodyReader)

	if err != nil {
		return nil, fmt.Errorf("Upstream.SaveToPath copy - %w", err)
	}

	if err := buf.Error(); err != nil {
		return nil, fmt.Errorf("Upstream.SaveToPath buffer - %w", err)
	}

	response := NewResponse(buf, res, remotePath)
	u.save(response, local, env)
	return response, nil
}

// We log the error here, because some cases won't care about this error
// and might just ignore it, but we still want to know about it
func (u *Upstream) save(response *Response, local string, env *Env) error {
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

	if err := gob.NewEncoder(f).Encode(response); err != nil {
		env.Error("Upstream.Save.encode_response").Err(err).Log()
		return err
	}
	return nil
}
