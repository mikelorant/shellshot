package terminal

import (
	"fmt"
	"io"
	"net/http"
	"os"

	// "time"

	"github.com/vito/midterm"
)

type Terminal struct {
	ptmx      *os.File
	vt        *midterm.Terminal
	cols      int
	rows      int
	debugPort int
}

const (
	defaultCols          = 80
	defaultRows          = 24
	defaultRaw           = true
	defaultCursorVisible = true
)

func New() *Terminal {
	term := &Terminal{
		cols: defaultCols,
		rows: defaultRows,
	}

	return term
}

func (t *Terminal) Start(opts ...func(*Terminal)) {
	for _, o := range opts {
		o(t)
	}

	t.vt = midterm.NewTerminal(t.rows, t.cols)
	t.vt.Raw = defaultRaw
	t.vt.CursorVisible = defaultCursorVisible

	if t.ptmx != nil {
		t.vt.ForwardResponses = t.ptmx

		go io.Copy(t.ptmx, os.Stdin)

		go func() {
			for {
				buf := make([]byte, 32*1024)

				n, err := t.ptmx.Read(buf)
				if err != nil {
					break
				}

				t.vt.Write(buf[:n])
			}
		}()
	}

	if t.debugPort != 0 {
		go func() {
			debugServer(t.vt, t.debugPort)
		}()
	}
}

// func (t *Terminal) Capture(ch chan bool) []string {
// 	done := make(chan bool, 1)
// 	var frames []string
//
// 	go func() {
// 		ticker := time.NewTicker(50 * time.Millisecond)
//
// 		for {
// 			select {
// 			case <-done:
// 				return
// 			case <-ticker.C:
// 				l := buf.Len()
// 				r := buf.Next(l)
//
// 				vt.Write(r)
//
// 				var frame bytes.Buffer
// 				vt.Render(&frame)
//
// 				frames = append(frames, frame.String())
// 			}
// 		}
// 	}()
// }

func debugServer(vt *midterm.Terminal, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", root(vt))

	addr := fmt.Sprintf(":%v", port)

	http.ListenAndServe(addr, mux)
}

func WithPTMX(ptmx *os.File) func(*Terminal) {
	return func(t *Terminal) {
		t.ptmx = ptmx
	}
}

func WithRows(rows int) func(*Terminal) {
	return func(t *Terminal) {
		t.rows = rows
	}
}

func WithCols(cols int) func(*Terminal) {
	return func(t *Terminal) {
		t.cols = cols
	}
}

func WithDebug(port int) func(*Terminal) {
	return func(t *Terminal) {
		t.debugPort = port
	}
}

func root(vt *midterm.Terminal) func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		// w.Header().Set("Content-Type", "text/html")

		fmt.Fprint(w, "<body>")
		fmt.Fprint(w, vt.HTML())
		fmt.Fprint(w, "</body>")
	}
}
