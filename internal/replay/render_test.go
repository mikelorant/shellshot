package replay

import (
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// frameHead starts every frame rendered with Clear: a synchronized update that
// hides the cursor and homes it.
const frameHead = ansiSyncBegin + ansiHideCursor + ansiHome

func TestRenderColor(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	green := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 2}}
	bold := snapshot.Style{Attrs: snapshot.Bold}
	spans := func(lines []string, sp ...[]snapshot.Span) snapshot.Snapshot {
		return snapshot.Snapshot{Rows: len(lines), Lines: lines, Spans: sp}
	}

	tests := []struct {
		name string
		snap snapshot.Snapshot
		opts RenderOptions
		want string
	}{
		{
			name: "no spans",
			snap: snapshot.Snapshot{Lines: []string{"abc"}},
			opts: RenderOptions{Color: true},
			want: "abc\n",
		},
		{
			name: "empty span list for a line",
			snap: spans([]string{"abc"}, nil),
			opts: RenderOptions{Color: true},
			want: "abc\n",
		},
		{
			name: "single span covering the line",
			snap: spans([]string{"abc"}, []snapshot.Span{{Start: 0, End: 3, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31mabc\x1b[0m\n",
		},
		{
			name: "two adjacent spans",
			snap: spans([]string{"abcd"}, []snapshot.Span{{Start: 0, End: 2, Style: red}, {Start: 2, End: 4, Style: green}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31mab\x1b[0m\x1b[32mcd\x1b[0m\n",
		},
		{
			name: "span in the middle keeps plain prefix and suffix",
			snap: spans([]string{"abcde"}, []snapshot.Span{{Start: 1, End: 4, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "a\x1b[31mbcd\x1b[0me\n",
		},
		{
			name: "gap between spans is plain",
			snap: spans([]string{"abcde"}, []snapshot.Span{{Start: 0, End: 1, Style: red}, {Start: 3, End: 5, Style: green}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31ma\x1b[0mbc\x1b[32mde\x1b[0m\n",
		},
		{
			name: "spans are rune indexes not byte indexes",
			snap: spans([]string{"世界abc"}, []snapshot.Span{{Start: 1, End: 2, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "世\x1b[31m界\x1b[0mabc\n",
		},
		{
			name: "span after multibyte runes",
			snap: spans([]string{"世界abc"}, []snapshot.Span{{Start: 3, End: 4, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "世界a\x1b[31mb\x1b[0mc\n",
		},
		{
			name: "palette 256 colour foreground and background",
			snap: spans([]string{"x"}, []snapshot.Span{{Start: 0, End: 1, Style: snapshot.Style{
				Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 208},
				Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 9},
			}}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[38;5;208;101mx\x1b[0m\n",
		},
		{
			name: "rgb foreground and background",
			snap: spans([]string{"x"}, []snapshot.Span{{Start: 0, End: 1, Style: snapshot.Style{
				Fg: snapshot.Color{Kind: snapshot.ColorRGB, R: 1, G: 2, B: 3},
				Bg: snapshot.Color{Kind: snapshot.ColorRGB, R: 4, G: 5, B: 6},
			}}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[38;2;1;2;3;48;2;4;5;6mx\x1b[0m\n",
		},
		{
			name: "attributes with colour",
			snap: spans([]string{"x"}, []snapshot.Span{{Start: 0, End: 1, Style: snapshot.Style{
				Attrs: snapshot.Bold | snapshot.Underline | snapshot.Strike,
				Fg:    snapshot.Color{Kind: snapshot.ColorPalette, Index: 1},
			}}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[1;4;9;31mx\x1b[0m\n",
		},
		{
			name: "styles reset per line",
			snap: spans([]string{"ab", "cd"}, []snapshot.Span{{Start: 1, End: 2, Style: bold}}, []snapshot.Span{{Start: 0, End: 1, Style: bold}}),
			opts: RenderOptions{Color: true},
			want: "a\x1b[1mb\x1b[0m\n\x1b[1mc\x1b[0md\n",
		},
		{
			name: "Color false ignores spans",
			snap: spans([]string{"abc"}, []snapshot.Span{{Start: 0, End: 3, Style: red}}),
			opts: RenderOptions{},
			want: "abc\n",
		},
		{
			name: "clear mode with colour keeps CRLF joins and cursor",
			snap: func() snapshot.Snapshot {
				s := spans([]string{"ab", "cd"}, []snapshot.Span{{Start: 0, End: 1, Style: red}}, []snapshot.Span{{Start: 1, End: 2, Style: green}})
				s.Cursor = snapshot.Cursor{X: 2, Y: 1, Visible: true}
				return s
			}(),
			opts: RenderOptions{Clear: true, Color: true},
			want: frameHead + "\x1b[31ma\x1b[0mb\x1b[K\r\n\x1b[Jc\x1b[32md\x1b[0m\x1b[2;3H\x1b[?25h" + ansiSyncEnd,
		},
		{
			name: "clear mode without colour ignores spans",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: 0, End: 1, Style: red}}),
			opts: RenderOptions{Clear: true},
			want: frameHead + "\x1b[Jab\x1b[1;1H" + ansiSyncEnd,
		},
		{
			name: "span end beyond line is clamped",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: 1, End: 9, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "a\x1b[31mb\x1b[0m\n",
		},
		{
			name: "negative start is clamped",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: -5, End: 1, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31ma\x1b[0mb\n",
		},
		{
			name: "span wholly outside line is dropped",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: 5, End: 9, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "ab\n",
		},
		{
			name: "empty and inverted spans are dropped",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: 1, End: 1, Style: red}, {Start: 2, End: 0, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "ab\n",
		},
		{
			name: "overlapping span is trimmed",
			snap: spans([]string{"abcd"}, []snapshot.Span{{Start: 0, End: 3, Style: red}, {Start: 1, End: 4, Style: green}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31mabc\x1b[0m\x1b[32md\x1b[0m\n",
		},
		{
			name: "zero style span is plain",
			snap: spans([]string{"ab"}, []snapshot.Span{{Start: 0, End: 1}}),
			opts: RenderOptions{Color: true},
			want: "ab\n",
		},
		{
			name: "fewer span lines than text lines",
			snap: spans([]string{"ab", "cd"}, []snapshot.Span{{Start: 0, End: 1, Style: red}}),
			opts: RenderOptions{Color: true},
			want: "\x1b[31ma\x1b[0mb\ncd\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Render(tt.snap, tt.opts); got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlayColor(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	f := snapshot.Frame{Snapshot: snapshot.Snapshot{Rows: 1, Lines: []string{"ab"}, Spans: [][]snapshot.Span{{{Start: 0, End: 1, Style: red}}}}}
	for _, tt := range []struct {
		color bool
		want  string
	}{{false, "ab\n"}, {true, "\x1b[31ma\x1b[0mb\n"}} {
		var out strings.Builder
		err := Play(t.Context(), &sliceSource{frames: []snapshot.Frame{f}}, &out, Options{Color: tt.color})
		if err != nil {
			t.Fatalf("Play() error = %v", err)
		}
		if out.String() != tt.want {
			t.Errorf("Color=%v output = %q, want %q", tt.color, out.String(), tt.want)
		}
	}
}

func TestRenderStyledTrailingBlanks(t *testing.T) {
	blue := snapshot.Style{Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 4}}
	s := snapshot.Snapshot{
		Cols: 10, Rows: 2,
		Lines: []string{"ab   ", "cd"},
		Spans: [][]snapshot.Span{{{Start: 2, End: 5, Style: blue}}, nil},
	}

	if got, want := Render(s, RenderOptions{Color: true}), "ab\x1b[44m   \x1b[0m\ncd\n"; got != want {
		t.Errorf("colour = %q, want %q", got, want)
	}
	if got, want := Render(s, RenderOptions{Color: true, Clear: true}),
		frameHead+"ab\x1b[44m   \x1b[0m\x1b[K\r\n\x1b[Jcd\x1b[1;1H"+ansiSyncEnd; got != want {
		t.Errorf("colour with clear = %q, want %q", got, want)
	}
	// Without colour the blanks show nothing, so they are not written.
	if got, want := Render(s, RenderOptions{}), "ab\ncd\n"; got != want {
		t.Errorf("plain = %q, want %q", got, want)
	}
	if got, want := Render(s, RenderOptions{Clear: true}), frameHead+"ab\x1b[K\r\n\x1b[Jcd\x1b[1;1H"+ansiSyncEnd; got != want {
		t.Errorf("plain with clear = %q, want %q", got, want)
	}
}
