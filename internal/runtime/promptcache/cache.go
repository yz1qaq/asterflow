package promptcache

import "sync"

// Cache stores compiled prompt products by promptID@env for the in-process runtime.
// Entries are replaced on publish and evicted when another process broadcasts invalidation.
type Cache struct {
	mp sync.Map
}

func NewCache() *Cache { return &Cache{} }

func cacheKey(promptID, env string) string {
	return promptID + "@" + env
}

// map[string]{Compiled}
func (c *Cache) Get(promptID, env string) (*Compiled, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.mp.Load(cacheKey(promptID, env))
	if !ok {
		return nil, false
	}
	return value.(*Compiled), true
}

func (c *Cache) Put(promptID, env string, compiled *Compiled) {
	if c == nil || compiled == nil {
		return
	}
	c.mp.Store(cacheKey(promptID, env), compiled)
}

func (c *Cache) Invalidate(promptID, env string) {
	if c == nil {
		return
	}
	c.mp.Delete(cacheKey(promptID, env))
}
