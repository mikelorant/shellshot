package emoji

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const (
	notoFontSHA256 = "15671215ab769fdc7162a045d56fd7d7e477c51b04e6b3c761d914d8fdd6cc44"
	notoFontSize   = 10730124
)

func TestNotoFontZstdDecompressesToPinnedUpstreamFont(t *testing.T) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	font, err := dec.DecodeAll(NotoFontZstd(), nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if len(font) != notoFontSize {
		t.Errorf("decompressed size = %d, want %d", len(font), notoFontSize)
	}
	sum := sha256.Sum256(font)
	if got := hex.EncodeToString(sum[:]); got != notoFontSHA256 {
		t.Errorf("sha256 = %s, want %s", got, notoFontSHA256)
	}
}
