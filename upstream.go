package assets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
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

	lr, err := NewLocalResponse(u, f)
	if err != nil {
		f.Close()
		env.Error("Upstream.LoadLocal.read").String("path", localPath).Err(err).Log()
		return nil
	}

	// callers can opt to ignore the expiration
	if !force {
		if int64(lr.meta.expires) < time.Now().Unix() {
			lr.Close()
			// no need to delete this file, because we expect our caller to go fetch
			// it from the upstream and save the result, overwriting this
			return nil
		}
	}

	return lr
}

func (u *Upstream) LoadLocalImage(localMetaPath string, localImagePath string, env *Env) *LocalResponse {
	f, err := os.Open(localMetaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			env.Error("Upstream.LoadLocalImage.Meta").String("path", localMetaPath).Err(err).Log()
		}
		return nil
	}

	// We have a local cache for this request...but things get complicated. Image
	// responses are, annoyingly, stored across 2 files: one containing the metadata
	// and one containing the actual image (this is necessary to easily interact
	// with vipsthumbnail). All we have so far is the metadata, so we also need to
	// setup the actual image. However, we might _only_ have metadata, say if
	// this is a 404.

	lr, err := NewLocalResponse(u, f)
	if err != nil {
		f.Close()
		env.Error("Upstream.LoadLocalImage.NewLocalResponse").String("path", localMetaPath).Err(err).Log()
		return nil
	}

	if lr.Type() == TYPE_GENERIC {
		// this isn't an image, the entire response is contained here
		return lr
	}

	// We have an image metadata file. We no longer need it (our lr has already
	// parsed the header, which is all this file contains). We need to load
	// the real image now.
	f.Close()
	f, err = os.Open(localImagePath)
	if err != nil {
		// this should not happen
		env.Error("Upstream.LoadLocalImage.Image").String("path", localImagePath).Err(err).Log()
		return nil
	}
	lr.file = f
	return lr
}

// Our caller wants to check if we have an origin image locally cached.
// We might have it, in which case we'll return exists == true to let it know
// the file exists. But we might have a non-image locally cached instead. This
// would happen when we previously tried to donwload the image and got a
// non-image response (we cache negative responses too), in which case we'll
// returna  LocalResponse so that the cached response can be sent to the client
// as-is, without any additional image processing.
func (u *Upstream) OriginImageCheck(localMetaPath string, env *Env) (*LocalResponse, uint32, error) {
	f, err := os.Open(localMetaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	// TODO: NewLocalResponse reads more from the file than we strictly need
	// here. Specifically, it reads/allocates both the content-type and cache-control
	// header, which we don't need here.

	// lr owns f
	lr, err := NewLocalResponse(u, f)
	if err != nil {
		// this should not happen, let's pretend the file simply doesn't exist
		env.Error("Upstream.OriginImageCheck").Err(err).String("path", localMetaPath).Log()
		return nil, 0, nil
	}

	expires := lr.meta.expires
	if int64(expires) < time.Now().Unix() {
		lr.Close()
		// our origin has expired
		return nil, 0, nil
	}

	if lr.Type() == TYPE_GENERIC {
		// This origin isn't an image, it's something else (a 404?), we should
		// return this to our client
		return lr, 0, nil
	}

	// We appear to have a valid origin image, there isn't anything we need from
	// it at this point
	lr.Close()

	return nil, expires, nil
}

func (u *Upstream) LocalResPath(remotePath string, extension string) string {
	root := u.cacheRoot

	// +3 for the subfolder that we'll use, which will be encodedPath[:2] + "/"
	prefixLength := len(root) + 3
	encodedLength := base64.RawURLEncoding.EncodedLen(len(remotePath))
	filnameLength := prefixLength + encodedLength
	fullLength := filnameLength + len(extension)

	// +4 for the .res  that we'll append
	dst := make([]byte, fullLength+4)
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
	dst[fullLength] = '.'
	dst[fullLength+1] = 'r'
	dst[fullLength+2] = 'e'
	dst[fullLength+3] = 's'

	return utils.B2S(dst)
}

// originPath: up-name/AZ/AZ123.jpg.res
// metaPath: up-name/AZ/AZ123_xform.jpg.res
// imagePath: up-name/AZ/AZ123_xform.jpg
// (xform can be nil)
func (u *Upstream) LocalImagePath(remotePath string, extension string, xform []byte) (string, string) {
	root := u.cacheRoot

	// +3 for the subfolder that we'll use, which will be encodedPath[:2] + "/"
	xformLength := len(xform)
	if xformLength > 0 {
		xformLength += 1
	}
	prefixLength := len(root) + 3
	encodedLength := base64.RawURLEncoding.EncodedLen(len(remotePath))
	filnameLength := prefixLength + encodedLength
	fullLength := filnameLength + xformLength + len(extension)

	// +4 for the .res  that we'll append
	dst := make([]byte, fullLength+4)
	copy(dst, root)

	base64.RawURLEncoding.Encode(dst[prefixLength:], utils.S2B(remotePath))

	// At this point, we have something like:
	//   {r, o, o, t, /, 0, 0, 0, e, n, c, o, d, e, d}
	// And we want to inject the 2 character, plus a slash, where the 0, 0, 0 is
	//   {r, o, o, t, /, e, n, /, e, n, c, o, d, e, d}
	dst[prefixLength-3] = dst[prefixLength]
	dst[prefixLength-2] = dst[prefixLength+1]
	dst[prefixLength-1] = '/'

	if xformLength > 0 {
		dst[filnameLength] = '_'
		copy(dst[filnameLength+1:], xform)
	}
	copy(dst[filnameLength+xformLength:], extension)
	dst[fullLength] = '.'
	dst[fullLength+1] = 'r'
	dst[fullLength+2] = 'e'
	dst[fullLength+3] = 's'

	metaPath := utils.B2S(dst)
	// image path is the metaPath without the trailing .res
	return metaPath, metaPath[:fullLength]
}

// Issues an http request to our upstream and saves the content locally.
// The content is saved as our own Response object (serialized from a RemoteResponse
// and loaded back into a LocalResponse)
func (u *Upstream) GetResponseAndSave(remotePath string, localPath string, env *Env) (http.Response, error) {
	owner := false

	res, err, _ := u.sf.Do(remotePath, func() (any, error) {
		owner = true
		remoteURL := u.baseURL + remotePath
		res, err := u.client.Get(remoteURL)
		if err != nil {
			return nil, log.ErrData(ERR_PROXY, err, map[string]any{"url": remoteURL})
		}

		return u.createAndSaveRemoteResponse(res, localPath, TYPE_GENERIC, env)
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
	lr := u.LoadLocalResponse(localPath, env, true)
	if lr == nil {
		env.Error("Upstream.GetSaveAndServe.LoadLocal").String("remote", remotePath).Log()
		return nil, errSingleflightLocalLoad
	}
	return lr, nil
}

func (u *Upstream) SaveOriginImage(remotePath string, localMetaPath string, localImagePath string, env *Env) (http.Response, uint32, error) {
	owner := false

	res, err, _ := u.sf.Do(remotePath, func() (any, error) {
		owner = true
		remoteURL := u.baseURL + remotePath
		res, err := u.client.Get(remoteURL)
		if err != nil {
			return nil, log.ErrData(ERR_PROXY, err, map[string]any{"url": remoteURL})
		}

		if res.StatusCode != 200 || !isImage(res) {
			return u.createAndSaveRemoteResponse(res, localMetaPath, TYPE_GENERIC, env)
		}

		body := res.Body
		defer body.Close()

		f, err := openForWrite(localImagePath, env)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		bodyLength, err := io.Copy(f, body)
		if err != nil {
			os.Remove(localImagePath)
			return nil, err
		}

		ttl := u.calculateTTL(res)
		meta := MetaFromResponse(res, ttl, TYPE_IMAGE, uint32(bodyLength))
		if err := u.save(meta, localMetaPath, env); err != nil {
			os.Remove(localImagePath)
			return nil, err
		}

		return meta.expires, err
	})

	if err != nil {
		return nil, 0, err
	}

	if expires, ok := res.(uint32); ok {
		// This is the case that we want: the expiration time of the origin. This
		// means we _did_ get an origin image and successfully saved it. We don't
		// want the full response/image because our caller wants to transform it
		// (hence it just wants it on disk)
		return nil, expires, nil
	}

	// Ideally, we shouldn't be here. We're only here because our above closure
	// returned a Response. The only reason it returned a Response is because
	// it didn't get an image (maybe it got a 404?). At this point, everything's
	// like a non-image asset. We're going to locally cache the response and
	// return it to the client as-is, whatever it is.

	// This is the goroutine that actually executed the HTTP request to the upstream
	// and thus we designate it as the owner of the response
	if owner {
		return res.(*RemoteResponse), 0, nil
	}

	// This was one of the goroutines that was blocked on the singleflight.
	// This cannot use the res.(*RemoteResponse) as our RemoteResponse cannot
	// be shared across goroutines. Instead, at this point, we expect the file
	// to be saved locally, so we can return a LocalResponse.
	lr := u.LoadLocalResponse(localMetaPath, env, true)
	if lr == nil {
		env.Error("Upstream.GetSaveAndServe.LoadLocal").String("remote", remotePath).Log()
		return nil, 0, errSingleflightLocalLoad
	}
	return lr, 0, nil
}

func (u *Upstream) TransformImage(originImagePath string, localMetaPath string, localImagePath string, xformArgs []string, expires uint32, env *Env) error {
	// TODO: optimize this (fewer allocs, singleflight, ...)
	args := make([]string, len(xformArgs)+3)
	args[0] = originImagePath
	args[1] = "-o"

	// vipsthumbnails wants a relative path to the origin
	// (it can take an absolute path too, but we support both absolute and
	// relative, so better to just give it the relative path)
	args[2] = path.Base(localImagePath)
	for i := 0; i < len(xformArgs); i++ {
		args[i+3] = xformArgs[i]
	}

	cmd := exec.Command(Config.VipsThumbnail, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s - %w", string(out), err)
	}

	contentType := ""
	ext := lowercase(filepath.Ext(localImagePath))

	switch ext {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	default:
		env.Error("TransformImage.extension").String("ext", ext).Log()
	}

	fi, err := os.Stat(localImagePath)
	if err != nil {
		os.Remove(localImagePath) // no point keeping this around if we can't figure it's size
		return log.ErrData(ERR_FS_STAT, err, map[string]any{"path": localImagePath})
	}

	maxAge := strconv.Itoa(int(expires) - int(time.Now().Unix()))

	meta := &Meta{
		tpe:          TYPE_IMAGE,
		status:       200,
		expires:      expires,
		contentType:  contentType,
		bodyLength:   uint32(fi.Size()),
		cacheControl: "public,max-age=" + maxAge, // TODO, this is an absolute value, it should be a TTL, duh
	}

	if err := u.save(meta, localMetaPath, env); err != nil {
		os.Remove(localImagePath) // no point keeping this around without a meta file
		return err
	}
	return nil
}

func (u *Upstream) createAndSaveRemoteResponse(res *gohttp.Response, localPath string, tpe byte, env *Env) (*RemoteResponse, error) {
	body := res.Body
	defer body.Close()

	buf := u.buffers.Checkout()
	_, err := io.Copy(buf, body)

	if err != nil {
		buf.Release()
		env.Error("Upstream.createAndSaveRemoteResponse.Copy").Err(err).Log()
		return nil, err
	}

	if err := buf.Error(); err != nil {
		buf.Release()
		env.Error("Upstream.createAndSaveRemoteResponse.Buffer").Err(err).Log()
		return nil, err
	}

	ttl := u.calculateTTL(res)
	rr := NewRemoteResponse(res, buf, ttl, tpe)
	u.save(rr, localPath, env)
	return rr, nil
}

// We log the error here, because some cases won't care about this error
// and might just ignore it, but we still want to know about it
func (u *Upstream) save(s Serializable, localPath string, env *Env) error {
	f, err := openForWrite(localPath, env)
	if err != nil {
		return err
	}

	defer f.Close()
	if err := s.Serialize(f); err != nil {
		env.Error("Upstream.saveMeta").String("path", localPath).Err(err).Log()
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

func isImage(res *gohttp.Response) bool {
	ct := lowercase(res.Header.Get("Content-Type"))
	return ct == "image/png" ||
		ct == "image/webp" ||
		ct == "image/jpeg" ||
		ct == "image/gif"
}
