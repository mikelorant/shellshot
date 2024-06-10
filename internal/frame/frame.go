package frame

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/wader/ansisvg/ansitosvg"
)

func SaveAsSVG(fs []string, dir string) error {
	for idx, f := range fs {
		svg, err := ToSVG(f)
		if err != nil {
			return fmt.Errorf("unable to convert frame to SVG: %w", err)
		}

		file := fmt.Sprintf("frame-%04d.svg", idx+1)
		loc := path.Join(dir, file)

		if err := os.WriteFile(loc, svg.Bytes(), 0o666); err != nil {
			return fmt.Errorf("unable to write file: %w", err)
		}
	}

	return nil
}

func ToSVG(frame string) (*bytes.Buffer, error) {
	var svg *bytes.Buffer

	if err := ansitosvg.Convert(strings.NewReader(frame), svg, ansitosvg.DefaultOptions); err != nil {
		return nil, fmt.Errorf("unable to convert frame: %w", err)
	}

	return svg, nil
}
