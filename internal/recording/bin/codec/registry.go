package codec

import (
	"errors"
	"fmt"
	"sync"
)

// Registry maps codec ids to implementations. It is safe for concurrent use.
type Registry struct {
	mu     sync.RWMutex
	codecs map[ID]Codec
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{codecs: make(map[ID]Codec)}
}

// DefaultRegistry returns a new registry holding the none and flate codecs
// (flate at flate.DefaultCompression). Each call returns an independent value.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	flateCodec, err := NewFlateCodec(DefaultFlateOptions())
	if err != nil {
		panic(err) // DefaultFlateOptions is always valid
	}
	for _, c := range []Codec{NoneCodec{}, flateCodec} {
		if err := r.Register(c); err != nil {
			panic(err)
		}
	}
	return r
}

// Register adds c under c.ID(). It fails with ErrDuplicateCodec if the id is
// already registered.
func (r *Registry) Register(c Codec) error {
	if c == nil {
		return errors.New("codec: cannot register nil codec")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := c.ID()
	if _, ok := r.codecs[id]; ok {
		return fmt.Errorf("%w: %d (%s)", ErrDuplicateCodec, uint64(id), id)
	}
	r.codecs[id] = c
	return nil
}

// Get returns the codec registered under id, or ErrUnknownCodec.
func (r *Registry) Get(id ID) (Codec, error) {
	r.mu.RLock()
	c, ok := r.codecs[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownCodec, uint64(id))
	}
	return c, nil
}
