package emulator

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/mikelorant/shellshot/internal/frame"
)

func (t *Terminal) HandlerForHTML() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		// w.Header().Set("Content-Type", "text/html")

		fmt.Fprintf(w, `
			<style>
			    span {
			        font-family: JetBrainsMono Nerd Font, sans-serif;
			    }
			</style>
		`)

		fmt.Fprint(w, "<body>")
		fmt.Fprint(w, t.vt.HTML())
		fmt.Fprint(w, "</body>")
	}
}

func (t *Terminal) HandlerForSVG() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer

		if err := t.vt.Render(&buf); err != nil {
			return
		}

		w.Header().Set("Content-Type", "image/svg+xml")

		svg, err := frame.ToSVG(buf.String())
		if err != nil {
			return
		}

		fmt.Fprint(w, svg.String())
	}
}
