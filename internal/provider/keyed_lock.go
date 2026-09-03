package provider

import "sync"

// keyedMutex hands out one mutex per key, so operations the API can only take
// serially per object still run in parallel across unrelated objects. The zero
// value is ready to use.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// lock takes the mutex for key and returns its unlock function, so a caller
// writes `defer km.lock(key)()`.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*sync.Mutex{}
	}
	mu, ok := k.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		k.locks[key] = mu
	}
	k.mu.Unlock()

	mu.Lock()
	return mu.Unlock
}
