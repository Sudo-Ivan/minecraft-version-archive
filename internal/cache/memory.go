package cache

import (
	"slices"
	"sync"
	"sync/atomic"

	"minecraft-version-archive/internal/constants"
)

type entry struct {
	key  string
	data []byte
	size int64
	hits atomic.Uint32
	prev *entry
	next *entry
}

type shard struct {
	mu       sync.Mutex
	capacity int
	maxBytes int64
	bytes    int64
	items    map[string]*entry
	head     *entry
	tail     *entry
}

type Memory struct {
	shards []shard
	count  uint32
}

func New(maxEntries int, maxBytes int64) *Memory {
	return NewSharded(maxEntries, maxBytes, constants.DefaultCacheShards)
}

func NewSharded(maxEntries int, maxBytes int64, shards int) *Memory {
	if shards < 1 {
		shards = 1
	}
	perEntries := maxEntries / shards
	if shards > 1 && perEntries < constants.MinShardCapacity {
		perEntries = constants.MinShardCapacity
	}
	if perEntries < 1 {
		perEntries = 1
	}
	perBytes := max(maxBytes/int64(shards), 1)
	m := &Memory{shards: make([]shard, shards), count: uint32(shards)}
	for i := range m.shards {
		m.shards[i] = shard{
			capacity: perEntries,
			maxBytes: perBytes,
			items:    make(map[string]*entry),
		}
	}
	return m
}

func (c *Memory) shard(key string) *shard {
	return &c.shards[shardIndex(key)%c.count]
}

// MaxEntryBytes is the largest value a single shard will retain. Entries
// bigger than this evict themselves on Put.
func (c *Memory) MaxEntryBytes() int64 {
	return c.shards[0].maxBytes
}

func (c *Memory) Get(key string) ([]byte, bool) {
	s := c.shard(key)
	s.mu.Lock()
	e, ok := s.items[key]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	if e.hits.Add(1)%constants.LRUTouchInterval == 0 {
		s.moveFront(e)
	}
	data := e.data
	s.mu.Unlock()
	return data, true
}

// Put stores data under key. The slice is stored without copying and must not
// be mutated by the caller afterwards. On update the stored slice is replaced
// by a fresh copy so readers holding the previous slice keep seeing it.
func (c *Memory) Put(key string, data []byte) {
	if len(data) == 0 {
		return
	}
	size := int64(len(data))
	s := c.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.items[key]; ok {
		s.bytes -= e.size
		e.data = slices.Clone(data)
		e.size = size
		e.hits.Store(0)
		s.bytes += size
		s.moveFront(e)
		s.evict()
		return
	}

	e := &entry{
		key:  key,
		data: data,
		size: size,
	}
	s.items[key] = e
	s.bytes += size
	s.pushFront(e)
	s.evict()
}

func (s *shard) evict() {
	for (len(s.items) > s.capacity || s.bytes > s.maxBytes) && s.tail != nil {
		s.remove(s.tail)
	}
}

func (s *shard) remove(e *entry) {
	delete(s.items, e.key)
	s.bytes -= e.size
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
}

func (s *shard) moveFront(e *entry) {
	if s.head == e {
		return
	}
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	s.pushFront(e)
}

func (s *shard) pushFront(e *entry) {
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	} else {
		s.tail = e
	}
	s.head = e
}

func (c *Memory) Stats() (entries int, bytes int64) {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		entries += len(s.items)
		bytes += s.bytes
		s.mu.Unlock()
	}
	return entries, bytes
}
