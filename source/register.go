package source

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register stores a Factory under the given name. It is typically called
// from an init() function within a source implementation package so that
// the source is automatically available once imported.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = factory
}

// New creates a Source by name using the provided config map. It returns
// an error when the name is unknown or the factory fails.
func New(name string, config map[string]string) (Source, error) {
	mu.RLock()
	factory, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("source %q is not registered (did you import its package?)", name)
	}
	return factory(config)
}
