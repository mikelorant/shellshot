package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// session returns the 80x24 session of the size budget: a command typed one
// character at a time, then eight lines of coloured output, 30 steps in all.
func session(t testing.TB) []timeline.Step {
	const cmd = "cargo build --release"
	var steps []timeline.Step
	screenOf := func(typed int, output []string) snapshot.Snapshot {
		lines := []string{esc + "1;32m$" + reset + " " + cmd[:typed]}
		lines = append(lines, output...)
		for len(lines) < 24 {
			lines = append(lines, "")
		}
		x, y := 2+typed, 0
		if len(output) > 0 {
			x, y = 0, 1+len(output)
		}
		return withCursor(screen(t, 80, lines...), x, y)
	}
	for i := 0; i <= 21; i++ {
		steps = append(steps, step(screenOf(min(i, len(cmd)), nil), 80*time.Millisecond))
	}
	var out []string
	for i := 0; i < 8; i++ {
		out = append(out, fmt.Sprintf("   "+esc+"1;34mCompiling"+reset+" crate%d v0.%d.0 (/work/crate%d)", i, i, i))
		steps = append(steps, step(screenOf(len(cmd), out), 300*time.Millisecond))
	}
	steps[len(steps)-1].Delay = 2 * time.Second
	return steps
}

func TestSessionHasThirtySteps(t *testing.T) {
	if n := len(session(t)); n != 30 {
		t.Fatalf("session has %d steps, want 30", n)
	}
}

var (
	keyframesRe = regexp.MustCompile(`@keyframes (a\d+)\{((?:[0-9.]+%\{[^}]*\})+)\}`)
	frameRe     = regexp.MustCompile(`([0-9.]+)%\{([^}]*)\}`)
	ruleRe      = regexp.MustCompile(`\.(a\d+)\{animation:(a\d+) ([0-9.]+)s steps\(1,end\) (infinite|\d+ forwards)\}`)
)

type kf struct {
	pct  float64
	decl string
}

// parsedCSS is the animation part of a document.
type parsedCSS struct {
	frames   map[string][]kf
	duration map[string]float64
	count    map[string]string
}

func parseCSS(t testing.TB, doc string) parsedCSS {
	t.Helper()
	p := parsedCSS{frames: map[string][]kf{}, duration: map[string]float64{}, count: map[string]string{}}
	for _, m := range keyframesRe.FindAllStringSubmatch(doc, -1) {
		for _, f := range frameRe.FindAllStringSubmatch(m[2], -1) {
			v, err := strconv.ParseFloat(f[1], 64)
			if err != nil {
				t.Fatal(err)
			}
			p.frames[m[1]] = append(p.frames[m[1]], kf{v, f[2]})
		}
	}
	for _, m := range ruleRe.FindAllStringSubmatch(doc, -1) {
		if m[1] != m[2] {
			t.Fatalf("class %s plays animation %s", m[1], m[2])
		}
		d, _ := strconv.ParseFloat(m[3], 64)
		p.duration[m[1]], p.count[m[1]] = d, m[4]
	}
	return p
}

// at returns the declarations in force at pct percent of the animation: those
// of the last keyframe at or before it, which is what steps(1,end) shows.
func (p parsedCSS) at(class string, pct float64) string {
	frames := p.frames[class]
	cur := frames[0].decl
	for _, f := range frames {
		if f.pct <= pct {
			cur = f.decl
		}
	}
	return cur
}

// strip is a row's strip: the nested <svg> with its class, or a plain row.
type playedRow struct {
	class    string
	variants [][]string // text of each variant
}

// rows reads the animated rows of doc: for each nested <svg y=...> the
// class of its <g> and the text of each variant (the tspans whose <text>
// baseline is within the variant's slab of cellH pixels).
func readStrips(t testing.TB, doc string, cellH int) (strips map[int]*playedRow) {
	t.Helper()
	strips = map[int]*playedRow{}
	dec := xml.NewDecoder(strings.NewReader(doc))
	var (
		cur      *playedRow
		inText   bool
		baseline int
		depth    int
		text     strings.Builder
		curTop   int
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return strips
		}
		if err != nil {
			t.Fatal(err)
		}
		switch tk := tok.(type) {
		case xml.StartElement:
			depth++
			attr := func(name string) string {
				for _, a := range tk.Attr {
					if a.Name.Local == name {
						return a.Value
					}
				}
				return ""
			}
			switch {
			case tk.Name.Local == "svg" && depth == 2:
				curTop, _ = strconv.Atoi(attr("y"))
				cur = &playedRow{}
				strips[curTop] = cur
			case tk.Name.Local == "g" && cur != nil:
				cur.class = attr("class")
			case tk.Name.Local == "text":
				inText = true
				baseline, _ = strconv.Atoi(attr("y"))
				text.Reset()
			}
		case xml.CharData:
			if inText {
				text.WriteString(strings.ReplaceAll(string(tk), " ", ""))
			}
		case xml.EndElement:
			depth--
			switch tk.Name.Local {
			case "text":
				inText = false
				if cur != nil {
					k := baseline / cellH
					for len(cur.variants) <= k {
						cur.variants = append(cur.variants, nil)
					}
					cur.variants[k] = append(cur.variants[k], text.String())
				}
			case "svg":
				if depth == 1 {
					cur = nil
				}
			}
		}
	}
}

func TestAnimatedKeyframesFollowTheStepTimes(t *testing.T) {
	r, g := newRenderer(t, raster.Options{Cursor: raster.CursorNone})
	lay := r.Layout()
	lines := func(a, b, c string) snapshot.Snapshot { return screen(t, 20, a, b, c) }
	steps := []timeline.Step{
		step(lines("one", "same", "x"), 250*time.Millisecond),
		step(lines("two", "same", "x"), 500*time.Millisecond),
		step(lines("two", "same", "y"), 125*time.Millisecond),
		step(lines("one", "same", "y"), 625*time.Millisecond),
	}
	doc := write(t, Options{Geometry: g}, 20, 3, steps...)
	css := parseCSS(t, doc)

	total := 1.5
	starts := []float64{0, 0.25, 0.75, 0.875}
	strips := readStrips(t, doc, lay.CellHeight)
	if len(strips) != 2 {
		t.Fatalf("%d animated rows, want 2 (rows 0 and 2 change, row 1 never does)\n%s", len(strips), normalise(doc))
	}
	for top, want := range map[int][]string{
		lay.Pad:                    {"one", "two", "two", "one"},
		lay.Pad + 2*lay.CellHeight: {"x", "x", "y", "y"},
	} {
		s := strips[top]
		if s == nil {
			t.Fatalf("no strip at y=%d", top)
		}
		if d := css.duration[s.class]; math.Abs(d-total) > 1e-9 {
			t.Errorf("row at y=%d lasts %vs, want %vs", top, d, total)
		}
		for i, text := range want {
			// sample the middle of step i
			mid := (starts[i] + map[bool]float64{true: total, false: starts[min(i+1, 3)]}[i == 3]) / 2
			decl := css.at(s.class, mid/total*100)
			var k int
			if _, err := fmt.Sscanf(decl, "transform:translateY(-%dpx)", &k); err != nil && decl != "transform:translateY(0)" {
				t.Fatalf("row %d step %d: unexpected declaration %q", top, i, decl)
			}
			slab := k / lay.CellHeight
			if slab >= len(s.variants) || len(s.variants[slab]) != 1 || s.variants[slab][0] != text {
				t.Errorf("row at y=%d, step %d (t=%.3fs): shows variant %d = %v, want %q", top, i, mid, slab, s.variants, text)
			}
		}
		// Keyframes sit exactly on the step starts where the variant changes.
		var got []float64
		for _, f := range css.frames[s.class] {
			got = append(got, f.pct/100*total)
		}
		if top == lay.Pad {
			if want := []float64{0, 0.25, 0.875}; !floatsNear(got, want, 1e-4) {
				t.Errorf("row 0 keyframe times %v, want %v", got, want)
			}
		} else if want := []float64{0, 0.75}; !floatsNear(got, want, 1e-4) {
			t.Errorf("row 2 keyframe times %v, want %v", got, want)
		}
	}
	if c := css.count[strips[lay.Pad].class]; c != "infinite" {
		t.Errorf("iteration count %q, want infinite", c)
	}
}

func floatsNear(a, b []float64, eps float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > eps {
			return false
		}
	}
	return true
}

func TestAnimatedBaseStateIsTheFirstFrame(t *testing.T) {
	r, g := newRenderer(t, raster.Options{Cursor: raster.CursorBar})
	lay := r.Layout()
	steps := []timeline.Step{
		step(withCursor(screen(t, 20, "first"), 5, 0), time.Second),
		step(withCursor(screen(t, 20, "second"), 6, 0), time.Second),
	}
	doc := write(t, Options{Geometry: g}, 20, 1, steps...)
	strips := readStrips(t, doc, lay.CellHeight)
	s := strips[lay.Pad]
	if s == nil || len(s.variants) != 2 || s.variants[0][0] != "first" || s.variants[1][0] != "second" {
		t.Fatalf("strip = %+v, want first then second", s)
	}
	// The cursor rectangle is at the first step's position when no CSS runs.
	want := fmt.Sprintf(`<rect class="a1" x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
		lay.Pad+5*lay.CellWidth, lay.Pad, lay.BarWidth, lay.CellHeight, hex(lay.Theme.Cursor))
	if !strings.Contains(doc, want) {
		t.Errorf("missing base cursor %s in\n%s", want, normalise(doc))
	}
}

func TestAnimatedFiniteLoopsEndOnTheLastFrame(t *testing.T) {
	_, g := newRenderer(t, raster.Options{Cursor: raster.CursorNone})
	steps := stepsOf(t, []timelineStep{{"a", time.Second}, {"b", time.Second}})
	doc := write(t, Options{Geometry: g, Loops: 3}, 10, 1, steps...)
	css := parseCSS(t, doc)
	if len(css.frames) != 1 {
		t.Fatalf("%d keyframe lists", len(css.frames))
	}
	for class, frames := range css.frames {
		if c := css.count[class]; c != "3 forwards" {
			t.Errorf("iteration count %q, want 3 forwards", c)
		}
		last, prev := frames[len(frames)-1], frames[len(frames)-2]
		if last.pct != 100 || last.decl != prev.decl {
			t.Errorf("final keyframe %+v should repeat the last frame's %+v at 100%%", last, prev)
		}
	}
}

func TestAnimatedSingleStepAndZeroDurationAreStatic(t *testing.T) {
	_, g := newRenderer(t, raster.Options{})
	one := write(t, Options{Geometry: g}, 10, 1, stepsOf(t, []timelineStep{{"only", time.Second}})...)
	zero := write(t, Options{Geometry: g}, 10, 1, stepsOf(t, []timelineStep{{"a", 0}, {"b", 0}})...)
	for name, doc := range map[string]string{"one step": one, "zero duration": zero} {
		if strings.Contains(doc, "@keyframes") || strings.Contains(doc, "<style") {
			t.Errorf("%s: animated:\n%s", name, doc)
		}
	}
	if !strings.Contains(zero, ">b<") {
		t.Error("zero duration should show the last step")
	}
}

func TestAnimatedRowsWithEqualKeyframesShareOneAnimation(t *testing.T) {
	_, g := newRenderer(t, raster.Options{Cursor: raster.CursorNone})
	mk := func(a, b string) snapshot.Snapshot { return screen(t, 10, a, b) }
	doc := write(t, Options{Geometry: g}, 10, 2,
		step(mk("a", "c"), time.Second), step(mk("b", "d"), time.Second))
	if n := strings.Count(doc, "@keyframes"); n != 1 {
		t.Errorf("%d @keyframes lists for two rows that change together, want 1", n)
	}
}

func TestAnimatedUnderlineCursorHasARectanglePerWidth(t *testing.T) {
	r, g := newRenderer(t, raster.Options{Cursor: raster.CursorUnderline})
	lay := r.Layout()
	narrow := withCursor(screen(t, 10, "ab世"), 1, 0)
	wide := withCursor(screen(t, 10, "ab世"), 2, 0)
	hidden := screen(t, 10, "ab世")
	doc := write(t, Options{Geometry: g}, 10, 1,
		step(narrow, time.Second), step(wide, time.Second), step(hidden, time.Second), step(narrow, time.Second))
	if n := strings.Count(doc, fmt.Sprintf(`fill="%s"`, hex(lay.Theme.Cursor))); n != 2 {
		t.Errorf("%d cursor rectangles, want 2 (width 1 and width 2)\n%s", n, normalise(doc))
	}
	css := parseCSS(t, doc)
	for class, frames := range css.frames {
		if len(frames) == 0 || frames[0].pct != 0 {
			t.Errorf("%s: keyframes do not start at 0%%: %v", class, frames)
		}
	}
	if !strings.Contains(doc, "visibility:hidden") || !strings.Contains(doc, "visibility:visible") {
		t.Errorf("cursor keyframes do not toggle visibility:\n%s", normalise(doc))
	}
}

func TestAnimatedKeyframePercentsKeepMillisecondPrecisionOnLongSessions(t *testing.T) {
	_, g := newRenderer(t, raster.Options{Cursor: raster.CursorNone})
	var steps []timeline.Step
	for i := 0; i < 400; i++ {
		steps = append(steps, step(screen(t, 10, fmt.Sprintf("line %d", i)), 20*time.Millisecond+time.Duration(i%3)*time.Millisecond*500))
	}
	doc := write(t, Options{Geometry: g}, 10, 1, steps...)
	css := parseCSS(t, doc)
	var frames []kf
	for _, f := range css.frames {
		frames = append(frames, f...)
	}
	if len(frames) != len(steps) {
		t.Fatalf("%d keyframes for %d distinct steps", len(frames), len(steps))
	}
	total := 0.0
	for _, st := range steps {
		total += st.Delay.Seconds()
	}
	var start time.Duration
	for i, st := range steps {
		want := start.Seconds() / total * 100
		if got := frames[i].pct; math.Abs(got-want)*total/100 > 0.0006 {
			t.Fatalf("keyframe %d at %v%%, step starts at %.5f%% (%.4fs off)", i, got, want, math.Abs(got-want)*total/100)
		}
		if i > 0 && frames[i].pct <= frames[i-1].pct {
			t.Fatalf("keyframe %d (%v%%) is not after keyframe %d (%v%%)", i, frames[i].pct, i-1, frames[i-1].pct)
		}
		start += st.Delay
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	steps := session(t)
	var first []byte
	for i := 0; i < 3; i++ {
		_, g := newRenderer(t, raster.Options{})
		var buf bytes.Buffer
		if err := NewWriter(Options{Geometry: g}).WriteSteps(&buf, 80, 24, steps); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = buf.Bytes()
		} else if !bytes.Equal(first, buf.Bytes()) {
			t.Fatal("two runs wrote different bytes")
		}
	}
}

func TestAnimatedGolden(t *testing.T) {
	_, g := newRenderer(t, raster.Options{Cursor: raster.CursorBar})
	steps := []timeline.Step{
		step(withCursor(screen(t, 12, esc+"32m$"+reset+" l", "", ""), 3, 0), 200*time.Millisecond),
		step(withCursor(screen(t, 12, esc+"32m$"+reset+" ls", "", ""), 4, 0), 200*time.Millisecond),
		step(withCursor(screen(t, 12, esc+"32m$"+reset+" ls", "a.txt", esc+"1mb"+reset), 0, 2), time.Second),
	}
	doc := write(t, Options{Geometry: g, Loops: 2}, 12, 3, steps...)
	golden(t, "animated_text.svg", normalise(doc))
}

// TestSessionSize holds the size of the 30-step 80x24 session to a budget:
// the size measured when the format was written plus 10%. A change that grows
// the output must raise the budget on purpose. The session is 22 versions of
// the prompt row and eight lines of output, so it is smaller than a screen
// full of text that changes at every step.
func TestSessionSize(t *testing.T) {
	const (
		measuredText   = 10941 // animation, text mode
		measuredPaths  = 35683 // animation, paths mode
		measuredStatic = 2841  // last frame, text mode
	)
	steps := session(t)
	for _, tc := range []struct {
		name     string
		o        Options
		measured int
	}{
		{"animated text", Options{Text: TextText}, measuredText},
		{"animated paths", Options{Text: TextPaths}, measuredPaths},
		{"static text", Options{Frame: Frame{Kind: FrameLast}}, measuredStatic},
	} {
		_, g := newRenderer(t, raster.Options{})
		tc.o.Geometry = g
		var buf bytes.Buffer
		if err := NewWriter(tc.o).WriteSteps(&buf, 80, 24, steps); err != nil {
			t.Fatal(err)
		}
		checkDocument(t, buf.String())
		t.Logf("%s: %d bytes (measured %d)", tc.name, buf.Len(), tc.measured)
		if limit := tc.measured + tc.measured/10; buf.Len() > limit {
			t.Errorf("%s: %d bytes exceeds the budget of %d (measured %d + 10%%)", tc.name, buf.Len(), limit, tc.measured)
		}
	}
}
