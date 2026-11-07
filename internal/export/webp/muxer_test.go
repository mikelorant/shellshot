package webp

import (
	"bytes"
	"image"
	"testing"
)

// TestMuxerByteExact writes a file from hand-built frames (fake VP8L
// payloads, one of odd length) and compares every byte.
func TestMuxerByteExact(t *testing.T) {
	s, buf := newSink(t, 300, 200, Options{Loops: 3})
	s.alpha = true
	s.frames = []frame{
		{rect: image.Rect(0, 0, 300, 200), delayMillis: 0x010203, vp8l: []byte{0x2f, 0xaa, 0xbb, 0xcc}},
		{rect: image.Rect(4, 10, 7, 11), delayMillis: 40, vp8l: []byte{0x2f, 0x01, 0x02}},
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		'R', 'I', 'F', 'F', 0x6c, 0, 0, 0, 'W', 'E', 'B', 'P', // 108 = 4 + 18 + 14 + 2 * 36
		'V', 'P', '8', 'X', 10, 0, 0, 0,
		0x12, 0, 0, 0, // animation + alpha
		0x2b, 0x01, 0x00, // width 300 - 1
		0xc7, 0x00, 0x00, // height 200 - 1
		'A', 'N', 'I', 'M', 6, 0, 0, 0,
		0, 0, 0, 0, // background
		3, 0, // loops
		'A', 'N', 'M', 'F', 28, 0, 0, 0, // 16 + 8 + 4
		0, 0, 0, // x / 2
		0, 0, 0, // y / 2
		0x2b, 0x01, 0x00, // width - 1
		0xc7, 0x00, 0x00, // height - 1
		0x03, 0x02, 0x01, // duration
		0x02, // no blend, dispose none
		'V', 'P', '8', 'L', 4, 0, 0, 0,
		0x2f, 0xaa, 0xbb, 0xcc,
		'A', 'N', 'M', 'F', 28, 0, 0, 0, // 16 + 8 + 3 + 1 pad
		2, 0, 0, // x / 2
		5, 0, 0, // y / 2
		2, 0, 0, // width - 1
		0, 0, 0, // height - 1
		40, 0, 0, // duration
		0x02,
		'V', 'P', '8', 'L', 3, 0, 0, 0,
		0x2f, 0x01, 0x02, 0x00, // padded
	}
	if len(want) != 116 {
		t.Fatalf("test bug: expected file is %d bytes, want 116", len(want))
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("file mismatch\n got % x\nwant % x", buf.Bytes(), want)
	}
}

func TestMuxerChunkSizes(t *testing.T) {
	for n := 0; n < 6; n++ {
		payload := bytes.Repeat([]byte{0x2f}, n)
		var buf bytes.Buffer
		rw := &riffWriter{w: &buf}
		rw.frame(0, 0, 1, 1, 10, payload)
		if rw.err != nil {
			t.Fatal(rw.err)
		}
		if buf.Len() != frameChunkSize(n) {
			t.Errorf("payload of %d bytes: wrote %d bytes, frameChunkSize says %d", n, buf.Len(), frameChunkSize(n))
		}
		if buf.Len()%2 != 0 {
			t.Errorf("payload of %d bytes: chunk has odd length %d", n, buf.Len())
		}
	}
}

func TestVP8LPayload(t *testing.T) {
	good := []byte("RIFF\x10\x00\x00\x00WEBPVP8L\x03\x00\x00\x00abc\x00")
	if got, err := vp8lPayload(good); err != nil || string(got) != "abc" {
		t.Errorf("vp8lPayload = %q, %v", got, err)
	}
	for name, bad := range map[string][]byte{
		"short":     []byte("RIFF"),
		"not webp":  []byte("RIFF\x10\x00\x00\x00WAVEVP8L\x03\x00\x00\x00abc\x00"),
		"extended":  []byte("RIFF\x10\x00\x00\x00WEBPVP8X\x03\x00\x00\x00abc\x00"),
		"truncated": []byte("RIFF\x10\x00\x00\x00WEBPVP8L\x09\x00\x00\x00abc\x00"),
	} {
		if _, err := vp8lPayload(bad); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
