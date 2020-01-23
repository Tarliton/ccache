package ccache

import (
	"strings"
	"sync"
	"time"
)

type bucket struct {
	sync.RWMutex
	lookup map[string]*Item
}

func (b *bucket) itemCount() int {
	b.RLock()
	defer b.RUnlock()
	return len(b.lookup)
}

func (b *bucket) get(key string) *Item {
	b.RLock()
	defer b.RUnlock()
	return b.lookup[key]
}

func (b *bucket) set(key string, value interface{}, duration time.Duration) (*Item, *Item) {
	expires := time.Now().Add(duration).UnixNano()
	item := newItem(key, value, expires)
	b.Lock()
	defer b.Unlock()
	existing := b.lookup[key]
	b.lookup[key] = item
	return item, existing
}

func (b *bucket) delete(key string) *Item {
	b.Lock()
	defer b.Unlock()
	item := b.lookup[key]
	delete(b.lookup, key)
	return item
}

// This is an expensive operation, so we do what we can to optimize it and limit
// the impact it has on concurrent operations. Specifically, we:
// 1 - Do an initial iteration to collect matches. This allows us to do the
//     "expensive" prefix check (on all values) using only a read-lock
// 2 - Do a second iteration, under write lock, for the matched results to do
//     the actual deletion

// Also, this is the only place where the Bucket is aware of cache detail: the
// deletables channel. Passing it here lets us avoid iterating over matched items
// again in the cache. Further, we pass item to deletables BEFORE actually removing
// the item from the map. I'm pretty sure this is 100% fine, but it is unique.
// (We do this so that the write to the channel is under the read lock and not the
// write lock)
func (b *bucket) deletePrefix(prefix string, deletables chan *Item) int {
	lookup := b.lookup
	items := make([]*Item, 0, len(lookup)/10)

	b.RLock()
	for key, item := range lookup {
		if strings.HasPrefix(key, prefix) {
			deletables <- item
			items = append(items, item)
		}
	}
	b.RUnlock()

	if len(items) == 0 {
		// avoid the write lock if we can
		return 0
	}

	b.Lock()
	defer b.Unlock()
	for _, item := range items {
		delete(lookup, item.key)
	}
	return len(items)
}

func (b *bucket) clear() {
	b.Lock()
	defer b.Unlock()
	b.lookup = make(map[string]*Item)
}
