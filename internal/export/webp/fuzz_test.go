package webp

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"
)

// FuzzSink drives the Sink with a canvas size, delays and pixel edits taken
// from the fuzz input and checks that the file parses and recomposites to
// the input frames.
func FuzzSink(f *testing.F) {
	f.Add([]byte{3, 5, 2, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	f.Add([]byte{1, 1, 1, 255, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add(bytes.Repeat([]byte{7, 200, 13}, 40))
	f.Fuzz(func(t *testing.T, data []byte) {
		next := func() int {
			if len(data) == 0 {
				return 0
			}
			b := data[0]
			data = data[1:]
			return int(b)
		}
		w, h := 1+next()%24, 1+next()%24
		s, buf := newSink(t, w, h, Options{Loops: next()})
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		var want []*image.NRGBA
		for frames := 1 + next()%6; frames > 0; frames-- {
			for edits := next() % 5; edits > 0; edits-- {
				x, y := next()%w, next()%h
				a := next()
				// Premultiplied colour: channels do not exceed alpha.
				c := color.RGBA{uint8(next() % (a + 1)), uint8(next() % (a + 1)), uint8(next() % (a + 1)), uint8(a)}
				img.SetRGBA(x, y, c)
			}
			delay := time.Duration(next()*next()) * time.Millisecond
			if err := s.AddFrame(img, delay); err != nil {
				t.Fatal(err)
			}
			if n := toNRGBA(img); len(want) == 0 || !bytes.Equal(want[len(want)-1].Pix, n.Pix) {
				want = append(want, n)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		file, err := parseWebP(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeFrames(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%d frames, want %d", len(got), len(want))
		}
		for i := range got {
			if !bytes.Equal(got[i].Pix, want[i].Pix) {
				t.Fatalf("frame %d differs", i)
			}
			if fr := file.frames[i]; fr.x%2 != 0 || fr.y%2 != 0 {
				t.Fatalf("frame %d at odd offset (%d,%d)", i, fr.x, fr.y)
			}
		}
	})
}
