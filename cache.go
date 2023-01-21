package assets

import (
	"hash/fnv"
	"sync"
	"time"
)

type NotFoundCache struct {
	buckets [16]*NotFoundCacheBucket
}

func NewNotFoundCache(max int) *NotFoundCache {
	maxBucketSize := max / 16
	pruneSize := maxBucketSize / 10
	return &NotFoundCache{
		buckets: [16]*NotFoundCacheBucket{
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
			NewNotFoundCacheBucket(maxBucketSize, pruneSize),
		},
	}
}

func (c *NotFoundCache) Get(path string) bool {
	return c.bucket(path).get(path)
}

func (c *NotFoundCache) Set(path string, ttl uint32) {
	c.bucket(path).set(path, ttl)
}

func (c *NotFoundCache) bucket(path string) *NotFoundCacheBucket {
	h := fnv.New32a()
	h.Write([]byte(path))
	return c.buckets[h.Sum32()&15]
}

type NotFoundCacheBucket struct {
	sync.RWMutex
	max   int
	prune int
	items map[string]uint32
}

func NewNotFoundCacheBucket(max int, prune int) *NotFoundCacheBucket {
	return &NotFoundCacheBucket{
		max:   max,
		prune: prune,
		items: make(map[string]uint32, max),
	}
}

func (b *NotFoundCacheBucket) get(path string) bool {
	b.RLock()
	expires, exists := b.items[path]
	b.RUnlock()

	if !exists {
		return false
	}

	if expires > uint32(time.Now().Unix()) {
		return true
	}

	// item has expired, remove it
	b.Lock()
	delete(b.items, path)
	b.Unlock()
	return false
}

func (b *NotFoundCacheBucket) set(path string, ttl uint32) {
	max := b.max
	expires := uint32(time.Now().Unix()) + ttl
	defer b.Unlock()
	b.Lock()

	items := b.items
	items[path] = expires
	if len(items) < max {
		return
	}

	i := 0
	prune := b.prune
	for key := range items {
		delete(items, key)
		if i += 1; i == prune {
			break
		}
	}
}
