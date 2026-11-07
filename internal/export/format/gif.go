package format

import (
	"github.com/michaellorant/shellshot/internal/export/gif"
	"github.com/michaellorant/shellshot/internal/export/pipeline"
)

// gifFormat returns the animated GIF format.
func gifFormat() Format {
	return Format{
		Name:       "gif",
		Extensions: []string{".gif"},
		New: func(s Settings) pipeline.SinkFactory {
			return gif.SinkFactory(gif.Options{Colors: s.GIFColors, Dither: s.GIFDither, Loops: s.Loops})
		},
	}
}
