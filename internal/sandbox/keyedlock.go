package sandbox

import "sync"

// keyedMutex serializes work per key while letting different keys proceed in
// parallel. Entries are never removed: one small mutex per bot is cheaper than
// reference counting.
type keyedMutex[K comparable] struct {
	mu    sync.Mutex
	locks map[K]*sync.Mutex
}

// Lock blocks until the key's mutex is held and returns its unlock function.
func (k *keyedMutex[K]) Lock(key K) (unlock func()) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[K]*sync.Mutex)
	}
	entry, ok := k.locks[key]
	if !ok {
		entry = &sync.Mutex{}
		k.locks[key] = entry
	}
	k.mu.Unlock()

	entry.Lock()
	return entry.Unlock
}
