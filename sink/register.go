package sink

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register stores a Factory under the given name. It is typically called
// from an init() function within a sink implementation package so that
// the sink is automatically available once imported.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = factory
}

// New creates a Sink by name using the provided config map. It returns
// an error when the name is unknown or the factory fails.
func New(name string, config map[string]string) (Sink, error) {
	mu.RLock()
	factory, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sink %q is not registered (did you import its package?)", name)
	}
	return factory(config)
}
