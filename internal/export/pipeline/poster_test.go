package pipeline

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestParsePoster(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Poster
		wantErr bool
	}{
		{in: "none", want: Poster{}},
		{in: "first", want: Poster{Kind: PosterFirst}},
		{in: "last", want: Poster{Kind: PosterLast}},
		{in: "12s", want: Poster{Kind: PosterAt, At: 12 * time.Second}},
		{in: "800ms", want: Poster{Kind: PosterAt, At: 800 * time.Millisecond}},
		{in: "0s", want: Poster{Kind: PosterAt}},
		{in: "-1s", wantErr: true},
		{in: "12", wantErr: true},
		{in: "", wantErr: true},
		{in: "middle", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParsePoster(tt.in)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), `"`+tt.in+`"`) {
					t.Errorf("ParsePoster(%q) = %+v, %v; want an error naming the value", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ParsePoster(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestPosterStep(t *testing.T) {
	t.Parallel()
	steps := []timeline.Step{{Delay: time.Second}, {Delay: 2 * time.Second}, {Delay: 3 * time.Second}}
	tests := []struct {
		name string
		p    Poster
		want int
	}{
		{"first", Poster{Kind: PosterFirst}, 0},
		{"last", Poster{Kind: PosterLast}, 2},
		{"none picks the last", Poster{}, 2},
		{"at the start", Poster{Kind: PosterAt}, 0},
		{"inside the first step", Poster{Kind: PosterAt, At: 999 * time.Millisecond}, 0},
		{"a step ends where the next begins", Poster{Kind: PosterAt, At: time.Second}, 1},
		{"inside the second", Poster{Kind: PosterAt, At: 2500 * time.Millisecond}, 1},
		{"at the end", Poster{Kind: PosterAt, At: 6 * time.Second}, 2},
		{"past the end", Poster{Kind: PosterAt, At: time.Hour}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Step(steps); got != tt.want {
				t.Errorf("Step = %d, want %d", got, tt.want)
			}
		})
	}
}

// posterExport is goldenExport with a poster. The recording has the screens
// a (0 to 100 ms), b (to 450 ms), c (to 900 ms) and a again (the hold).
func posterExport(t *testing.T, p Poster) []byte {
	t.Helper()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"), frame(100*time.Millisecond, "b"), frame(300*time.Millisecond, "b"),
		frame(450*time.Millisecond, "c"), frame(900*time.Millisecond, "a"),
	}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, patternRenderer{}, &buf, Options{
		Timeline:    timeline.Options{Hold: 70 * time.Second},
		Loops:       3,
		Compression: png.BestCompression,
		Poster:      p,
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rendered returns the image the patternRenderer draws for screen label.
func rendered(t *testing.T, label string) *image.RGBA {
	t.Helper()
	img, err := patternRenderer{}.Render(frame(0, label).Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func samePicture(t *testing.T, got image.Image, want *image.RGBA) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("size %v, want %v", got.Bounds().Size(), want.Bounds().Size())
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			if g, w := color.NRGBAModel.Convert(got.At(x, y)), color.NRGBAModel.Convert(want.At(x, y)); g != w {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, g, w)
			}
		}
	}
}

// --poster none and --poster first write the file Export wrote before
// posters existed: the first frame is the default image.
func TestExportPosterNoneAndFirstMatchGolden(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile("testdata/apng_golden.apng")
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]Poster{"none": {Kind: PosterNone}, "first": {Kind: PosterFirst}, "at 0": {Kind: PosterAt}} {
		if got := posterExport(t, p); !bytes.Equal(got, want) {
			t.Errorf("poster %s: output changed: got %d bytes, want %d", name, len(got), len(want))
		}
	}
}

func TestExportPosterIsTheDefaultImage(t *testing.T) {
	t.Parallel()
	golden := decode(t, posterExport(t, Poster{}))
	tests := []struct {
		name  string
		p     Poster
		label string
	}{
		{"last", Poster{Kind: PosterLast}, "a"},
		{"time in the second step", Poster{Kind: PosterAt, At: 200 * time.Millisecond}, "b"},
		{"time in the third step", Poster{Kind: PosterAt, At: 500 * time.Millisecond}, "c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := posterExport(t, tt.p)
			got, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			samePicture(t, got, rendered(t, tt.label))

			// The animation is the same as without a poster, after the
			// default image that kettek lists first.
			a := decode(t, out)
			if !a.Frames[0].IsDefault {
				t.Fatal("first kettek frame is not the default image")
			}
			if g, w := delays(kettekAnimation(a)), delays(golden); !equalDurations(g, w) {
				t.Errorf("animation delays = %v, want %v", g, w)
			}
		})
	}
}

// kettekAnimation returns a without its default image.
func kettekAnimation(a kettek.APNG) kettek.APNG {
	a.Frames = a.Frames[1:]
	return a
}

// A poster is only for sinks that have one.
func TestExportPosterNeedsAPosterSink(t *testing.T) {
	t.Parallel()
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{}, &buf, Options{
		Timeline: timeline.Options{Hold: time.Second},
		NewSink:  factoryFor(&sink, plain),
		Poster:   Poster{Kind: PosterLast},
	})
	if err == nil || !strings.Contains(err.Error(), "poster") {
		t.Fatalf("err = %v, want an error about the poster", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes after a failed export", buf.Len())
	}
	if len(sink.frames) != 0 || sink.closes != 1 {
		t.Errorf("sink got %d frames and %d closes, want 0 and 1", len(sink.frames), sink.closes)
	}
}
