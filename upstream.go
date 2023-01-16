package assets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/buffer"
	"src.goblgobl.com/utils/http"
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
	cacheRoot []byte

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

	// xform parameter -> vips command line
	vipsTransforms map[string][]string
}

func NewUpstream(name string, config *upstreamConfig) (*Upstream, error) {
	cacheRoot, err := filepath.Abs(path.Join(Config.CacheRoot, name) + "/")
	if err != nil {
		return nil, fmt.Errorf("Failed to get absolute path (%s) - %w", Config.CacheRoot, err)
	}
	cacheRoot += "/"

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
		name:           name,
		sf:             new(singleflight.Group),
		baseURL:        config.BaseURL,
		client:         &gohttp.Client{},
		cacheRoot:      []byte(cacheRoot),
		defaultTTL:     uint32(defaultTTL),
		ttls:           ttls,
		vipsTransforms: config.VipsTransforms,

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

func (u *Upstream) LoadLocalResponse(localPath string, env *Env, force bool) *LocalResponse {
	f, err := os.Open(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		env.Error("Upstream.LoadLocal.open").String("path", localPath).Err(err).Log()
		return nil
	}

	res, err := NewLocalResponse(u, f)
	if err != nil {
		env.Error("Upstream.LoadLocal.read").String("path", localPath).Err(err).Log()
		return nil
	}

	// callers can opt to ignore the expiration
	if !force {
		expires := int(res.expires)
		if expires != 0 && int64(expires) < time.Now().Unix() {
			res.Close()
			// no need to delete this file, because we expect our caller to go fetch
			// it from the upstream and save the result, overwriting this
			return nil
		}
	}

	return res
}

func (u *Upstream) LoadLocalImage(localPath string, env *Env) http.Response {
	f, err := os.Open(localPath)
	if err != nil {
		if !os.IsNotExist(err) {
			env.Error("Upstream.LoadLocalImage").String("path", localPath).Err(err).Log()
		}
		return nil
	}

	return NewImageResponse(f)
}

// This function is a bit messy. Our caller wants to check if we have an
// origin image locally cached. We might have it, in which case we'll return
// exists == true to let it know the file exists.
// But we might have a non-image locally cached instead. This would happen when
// we previously tried to donwload the image and got a non-image response (we
// cache negative responses too), in which case we'll returna  LocalResponse
// so that the cached response can be sent to the client as-is, without any
// additional image processing.
func (u *Upstream) LocalImageCheck(localPath string, env *Env) (*LocalResponse, bool, error) {
	f, err := os.Open(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	// f doesn't have to be closed if we have a LocalResponse, since the
	// LocalResponse takes ownership of the file and will close it later.

	lr, err := NewLocalResponse(u, f)
	if err == nil {
		// We have a valid LocalResponse. This means we've cached a non-image
		// response (which we'll just want to return to the client as-is)
		return lr, false, nil
	}

	// We have a file and it doesn't appear to be a LocalResponse, we'll assume
	// this is an image and signal so to our caller
	f.Close()
	return nil, true, nil
}

func (u *Upstream) LocalPath(remotePath string, extension string) string {
	root := u.cacheRoot

	// +3 for the subfolder that we'll use, which will be encodedPath[:2] + "/"
	prefixLength := len(root) + 3
	encodedLength := base64.RawURLEncoding.EncodedLen(len(remotePath))
	filnameLength := prefixLength + encodedLength

	dst := make([]byte, filnameLength+len(extension))
	copy(dst, root)

	base64.RawURLEncoding.Encode(dst[prefixLength:], utils.S2B(remotePath))

	// At this point, we have something like:
	//   {r, o, o, t, /, 0, 0, 0, e, n, c, o, d, e, d}
	// And we want to inject the 2 character, plus a slash, where the 0, 0, 0 is
	//   {r, o, o, t, /, e, n, /, e, n, c, o, d, e, d}
	dst[prefixLength-3] = dst[prefixLength]
	dst[prefixLength-2] = dst[prefixLength+1]
	dst[prefixLength-1] = '/'

	copy(dst[filnameLength:], extension)

	return utils.B2S(dst)
}

// Issues an http request to our upstream and saves the content locally.
// The content is saved as our own Response object (serialized from a RemoteResponse
// and loaded back into a LocalResponse)
func (u *Upstream) GetResponseAndSave(remotePath string, localPath string, env *Env) (Response, error) {
	owner := false

	res, err, _ := u.sf.Do(remotePath, func() (any, error) {
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

		return u.createAndSaveRemoteResponse(res, localPath, env)
	})

	if err != nil {
		return nil, err
	}

	// This is the goroutine that actually executed the HTTP request to the upstream
	// and thus we designate it as the owner of the response
	if owner {
		return res.(*RemoteResponse), nil
	}

	// This was one of the goroutines that was blocked on the singleflight.
	// This cannot use the res.(*RemoteResponse) as our RemoteResponse cannot
	// be shared across goroutines. Instead, at this point, we expect the file
	// to be saved locally, so we can return a LocalResponse.
	localRes := u.LoadLocalResponse(localPath, env, true)
	if localRes == nil {
		env.Error("Upstream.GetSaveAndServe.LoadLocal").String("remote", remotePath).Log()
		return nil, errSingleflightLocalLoad
	}
	return localRes, nil
}

// This doesn't deal with RemoteResponses (or LocalResponses) but rather the
// raw body of the upstream response. This is necessary because we'll want to
// do image transformation directly on these.
func (u *Upstream) SaveOriginImage(remotePath string, localPath string, env *Env) (Response, error) {
	owner := false

	res, err, _ := u.sf.Do(remotePath, func() (any, error) {
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

		if res.StatusCode != 200 {
			return u.createAndSaveRemoteResponse(res, localPath, env)
		}

		f, err := openForWrite(localPath, env)
		if err != nil {
			return nil, err
		}
		body := res.Body
		defer body.Close()
		_, err = io.Copy(f, body)
		return nil, err
	})

	if err != nil {
		return nil, err
	}

	if res == nil {
		// This is the case that we want. No error. No response. No response because
		// we detected an image and saved it locally, which is what this function
		// is supposed to do (we're not returning our image as a response because,
		// presumably our caller wants to apply some transformations to it)
		return nil, nil
	}

	// Ideally, we shouldn't be here. We're only here because our above closure
	// returned a Response. The only reason it returned a Response is because
	// it didn't get an image (maybe it got a 404?). At this point, everything's
	// like a non-image asset. We're going to locally cache the response and
	// return it to the client as-is, whatever it is.

	// This is the goroutine that actually executed the HTTP request to the upstream
	// and thus we designate it as the owner of the response
	if owner {
		return res.(*RemoteResponse), nil
	}

	// This was one of the goroutines that was blocked on the singleflight.
	// This cannot use the res.(*RemoteResponse) as our RemoteResponse cannot
	// be shared across goroutines. Instead, at this point, we expect the file
	// to be saved locally, so we can return a LocalResponse.
	localRes := u.LoadLocalResponse(localPath, env, true)
	if localRes == nil {
		env.Error("Upstream.GetSaveAndServe.LoadLocal").String("remote", remotePath).Log()
		return nil, errSingleflightLocalLoad
	}
	return localRes, nil
}

func (u *Upstream) createAndSaveRemoteResponse(res *gohttp.Response, localPath string, env *Env) (*RemoteResponse, error) {
	body := res.Body
	defer body.Close()

	buf := u.buffers.Checkout()
	_, err := io.Copy(buf, body)

	if err != nil {
		buf.Release()
		env.Error("Upstream.CreateAndSaveRemoteResponse.Copy").Err(err).Log()
		return nil, err
	}

	if err := buf.Error(); err != nil {
		buf.Release()
		env.Error("Upstream.CreateAndSaveRemoteResponse.Buffer").Err(err).Log()
		return nil, err
	}

	ttl := u.calculateTTL(res)
	response := NewRemoteResponse(res, buf, ttl)
	u.saveResponse(response, localPath, env)
	return response, nil
}

// We log the error here, because some cases won't care about this error
// and might just ignore it, but we still want to know about it
func (u *Upstream) saveResponse(response *RemoteResponse, localPath string, env *Env) error {
	f, err := openForWrite(localPath, env)
	if err != nil {
		return err
	}

	defer f.Close()
	if err := response.Serialize(f); err != nil {
		env.Error("Upstream.SaveResponse").String("path", localPath).Err(err).Log()
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

func openForWrite(local string, env *Env) (*os.File, error) {
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	f, err := os.OpenFile(local, flag, 0600)
	if err != nil {
		os.MkdirAll(path.Dir(local), 0700)
		f, err = os.OpenFile(local, flag, 0600)
		if err != nil {
			env.Error("openForWrite").String("path", local).Err(err).Log()
			return nil, err
		}
	}

	return f, nil
}
