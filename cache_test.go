package assets

import (
	"strconv"
	"testing"

	"src.goblgobl.com/tests/assert"
)

func Test_NotFoundCache_GetAndSet(t *testing.T) {
	c := NewNotFoundCache(100)
	assert.False(t, c.Get("a path"))

	c.Set("a path", 3)
	assert.True(t, c.Get("a path"))

	c.Set("a path", 0)
	assert.False(t, c.Get("a path"))
}

func Test_NotFoundCache_LimitsSize(t *testing.T) {
	c := NewNotFoundCache(160)
	for i := 0; i < 500; i++ {
		c.Set(strconv.Itoa(i), 100)
	}
	for _, bucket := range c.buckets {
		l := len(bucket.items)
		assert.True(t, l < 16)
	}
}
