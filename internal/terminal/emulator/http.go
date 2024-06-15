package emulator

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/mikelorant/shellshot/internal/frame"
)

func (e *Emulator) HandlerForHTML() func(w http.ResponseWriter, _ *http.Request) {
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
		fmt.Fprint(w, e.vt.HTML())
		fmt.Fprint(w, "</body>")
	}
}

func (e *Emulator) HandlerForSVG() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer

		if err := e.vt.Render(&buf); err != nil {
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
