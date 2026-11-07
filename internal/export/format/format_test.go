package format

import (
	"bytes"
	"io"
	"slices"
	"testing"

	"github.com/michaellorant/shellshot/internal/export/pipeline"
)

func TestDefaultRegistersAPNGAndSVG(t *testing.T) {
	r := Default()
	if got := r.Names(); !slices.Equal(got, []string{"apng", "gif", "svg", "webp"}) {
		t.Errorf("Names = %v, want [apng gif svg webp]", got)
	}
	if f, ok := r.ByExtension("shot.SVG"); !ok || f.Name != "svg" || f.New != nil || f.NewSteps == nil {
		t.Errorf("ByExtension(shot.SVG) = %+v, %v; want the steps format svg", f, ok)
	}
	if f, _ := r.Lookup("apng"); f.NewSteps != nil || f.New == nil {
		t.Error("apng must be a sink format")
	}
	f, ok := r.Lookup("apng")
	if !ok || f.Name != "apng" {
		t.Fatalf("Lookup(apng) = %+v, %v", f, ok)
	}
	if !slices.Equal(f.Extensions, []string{".png", ".apng"}) {
		t.Errorf("Extensions = %v, want [.png .apng]", f.Extensions)
	}
}

func TestDefaultReturnsIndependentRegistries(t *testing.T) {
	a, b := Default(), Default()
	if a == b {
		t.Error("Default returned the same registry twice")
	}
	a.formats[0].Name = "changed"
	if _, ok := b.Lookup("apng"); !ok {
		t.Error("changing one registry changed another")
	}
}

func TestLookupIgnoresCaseAndRejectsUnknown(t *testing.T) {
	r := Default()
	if _, ok := r.Lookup("APNG"); !ok {
		t.Error("Lookup(APNG) not found")
	}
	for _, name := range []string{"", "tiff", "png", "apng "} {
		if _, ok := r.Lookup(name); ok {
			t.Errorf("Lookup(%q) found a format", name)
		}
	}
}

func TestByExtension(t *testing.T) {
	r := Default()
	for _, path := range []string{"a.png", "a.apng", "dir/a.PNG", "A.ApNg", "x.y.png", "/t/.png"} {
		if f, ok := r.ByExtension(path); !ok || f.Name != "apng" {
			t.Errorf("ByExtension(%q) = %q, %v; want apng", path, f.Name, ok)
		}
	}
	for _, path := range []string{"", "a", "a.tiff", "a.png.bak", "png", "dir.png/a", "-"} {
		if f, ok := r.ByExtension(path); ok {
			t.Errorf("ByExtension(%q) = %q; want none", path, f.Name)
		}
	}
}

func TestNewRegistryKeepsOrderAndCopiesInput(t *testing.T) {
	in := []Format{{Name: "one", Extensions: []string{".1"}}, {Name: "two", Extensions: []string{".2", ".two"}}}
	r := NewRegistry(in...)
	in[0].Name = "changed"
	if got := r.Names(); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("Names = %v, want [one two]", got)
	}
	if f, ok := r.ByExtension("a.TWO"); !ok || f.Name != "two" {
		t.Errorf("ByExtension(a.TWO) = %q, %v; want two", f.Name, ok)
	}
}

func TestAPNGFactoryBuildsAPNGSink(t *testing.T) {
	f, _ := Default().Lookup("apng")
	sink, err := f.New(Settings{Loops: 2})(io.Discard, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sink.(*pipeline.APNGSink); !ok {
		t.Errorf("sink is %T, want *pipeline.APNGSink", sink)
	}
	var buf bytes.Buffer
	if _, err := f.New(Settings{})(&buf, 0, 0); err == nil {
		t.Error("a 0x0 canvas was accepted")
	}
}

func TestGIFFormatIsRegistered(t *testing.T) {
	r := Default()
	for _, path := range []string{"a.gif", "dir/A.GIF"} {
		if f, ok := r.ByExtension(path); !ok || f.Name != "gif" {
			t.Errorf("ByExtension(%q) = %q, %v; want gif", path, f.Name, ok)
		}
	}
	f, ok := r.Lookup("GIF")
	if !ok || f.Extensions[0] != ".gif" {
		t.Fatalf("Lookup(GIF) = %+v, %v", f, ok)
	}
}

func TestGIFFactoryPassesSettingsToTheSink(t *testing.T) {
	f, _ := Default().Lookup("gif")
	for _, s := range []Settings{{}, {Loops: 3, GIFColors: 16, GIFDither: true}} {
		sink, err := f.New(s)(io.Discard, 4, 4)
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		if _, ok := sink.(pipeline.DelayLimiter); !ok {
			t.Errorf("%+v: the GIF sink has no MaxDelay", s)
		}
	}
	if _, err := f.New(Settings{GIFColors: 1})(io.Discard, 4, 4); err == nil {
		t.Error("GIFColors 1 was accepted")
	}
	if _, err := f.New(Settings{})(io.Discard, 0, 0); err == nil {
		t.Error("a 0x0 canvas was accepted")
	}
}
