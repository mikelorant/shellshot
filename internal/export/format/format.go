// Package format is the registry of export output formats. Each format is a
// name, the file extensions it owns and a constructor for the
// pipeline.SinkFactory that encodes it, so a new raster format is one small
// package plus one line in Default. It is pure Go (no cgo, no cobra).
package format

import (
	"image/png"
	"path/filepath"
	"strings"

	"github.com/michaellorant/shellshot/internal/export/pipeline"
)

// Settings holds the export knobs the formats share. A format ignores the
// ones it has no use for. A per-format option becomes a field here.
type Settings struct {
	// Loops is the number of times the animation plays; 0 means forever.
	Loops int
	// Compression is the PNG compression level of an APNG's frames.
	Compression png.CompressionLevel
}

// Format describes one output format.
type Format struct {
	// Name is the lower-case name --format takes, such as "apng".
	Name string
	// Extensions are the file extensions of the format, lower-case with the
	// leading dot. The first is the one used for a default output name.
	Extensions []string
	// New returns the factory of the format's pipeline.Sink for settings s.
	New func(s Settings) pipeline.SinkFactory
}

// Registry is an ordered set of formats.
type Registry struct {
	formats []Format
}

// NewRegistry returns a registry of the given formats, in the given order.
func NewRegistry(formats ...Format) *Registry {
	return &Registry{formats: append([]Format(nil), formats...)}
}

// Default returns a new registry of every built-in format.
func Default() *Registry {
	return NewRegistry(
		Format{
			Name:       "apng",
			Extensions: []string{".png", ".apng"},
			New: func(s Settings) pipeline.SinkFactory {
				return pipeline.APNGSinkFactory(s.Loops, s.Compression)
			},
		},
	)
}

// Lookup returns the format called name, ignoring case.
func (r *Registry) Lookup(name string) (Format, bool) {
	for _, f := range r.formats {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return Format{}, false
}

// ByExtension returns the format that owns the extension of path, ignoring
// case. It reports false for a path without an extension or with an unknown
// one.
func (r *Registry) ByExtension(path string) (Format, bool) {
	ext := filepath.Ext(path)
	if ext == "" {
		return Format{}, false
	}
	for _, f := range r.formats {
		for _, e := range f.Extensions {
			if strings.EqualFold(e, ext) {
				return f, true
			}
		}
	}
	return Format{}, false
}

// Names returns the format names in registration order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.formats))
	for i, f := range r.formats {
		names[i] = f.Name
	}
	return names
}
