package emoji

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/image/font/sfnt"
)

const (
	appleSha256File   = "assets/AppleColorEmoji-shellshot.ttf.sha256"
	emojiTestList     = "../../../third_party/apple-emoji-ttf/sequences/emoji-test.txt"
	minAppleCoverage  = 0.99
	appleFontMaxBytes = 20 << 20
)

// decompressedAppleFont returns the embedded font, skipping the test when the
// asset is not committed.
func decompressedAppleFont(t *testing.T) []byte {
	t.Helper()
	compressed := AppleFontZstd()
	if len(compressed) == 0 {
		t.Skip("Apple Color Emoji asset is not present")
	}
	if len(compressed) > appleFontMaxBytes {
		t.Errorf("embedded asset is %d bytes, over the %d byte budget", len(compressed), appleFontMaxBytes)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	data, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	return data
}

func TestAppleAssetChecksum(t *testing.T) {
	data := decompressedAppleFont(t)
	want, err := os.ReadFile(appleSha256File)
	if err != nil {
		t.Skipf("no checksum file: %v", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.HasPrefix(string(want), got) {
		t.Errorf("decompressed font sha256 = %s, %s says %s", got, appleSha256File, strings.Fields(string(want))[0])
	}
}

func TestAppleAssetParsesAndCoversEmojiTest(t *testing.T) {
	data := decompressedAppleFont(t)
	font, err := sfnt.Parse(data)
	if err != nil {
		t.Fatalf("sfnt.Parse: %v", err)
	}
	tbls, err := readTables(data)
	if err != nil {
		t.Fatalf("readTables: %v", err)
	}
	for _, tag := range []string{"CBLC", "CBDT", "GSUB", "cmap"} {
		if tbls[tag] == nil {
			t.Errorf("table %s missing", tag)
		}
	}
	if os2 := tbls["OS/2"]; len(os2) < 10 || uint16(os2[8])<<8|uint16(os2[9]) != 4 {
		t.Errorf("OS/2 fsType must stay 4 (preview & print) as in the source font")
	}
	ligs, err := parseLigatures(tbls)
	if err != nil {
		t.Fatal(err)
	}

	list, err := os.Open(filepath.FromSlash(emojiTestList))
	if err != nil {
		t.Skipf("emoji-test.txt not available (init third_party/apple-emoji-ttf): %v", err)
	}
	defer list.Close()
	sequences := fullyQualifiedSequences(t, list)
	if len(sequences) < 3000 {
		t.Fatalf("read only %d fully-qualified sequences", len(sequences))
	}

	var buf sfnt.Buffer
	resolved := 0
	var missing []string
	for _, seq := range sequences {
		if resolvesToOneGlyph(t, font, &buf, ligs, seq) {
			resolved++
		} else if len(missing) < 10 {
			missing = append(missing, string(seq))
		}
	}
	coverage := float64(resolved) / float64(len(sequences))
	t.Logf("%d of %d fully-qualified sequences resolve (%.2f%%); unresolved e.g. %q",
		resolved, len(sequences), 100*coverage, missing)
	if coverage < minAppleCoverage {
		t.Errorf("coverage %.2f%% below %.0f%%", 100*coverage, 100*minAppleCoverage)
	}
}

// fullyQualifiedSequences reads the "fully-qualified" lines of emoji-test.txt.
func fullyQualifiedSequences(t *testing.T, r io.Reader) [][]rune {
	t.Helper()
	var out [][]rune
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		codes, status, ok := strings.Cut(line, ";")
		if !ok || strings.TrimSpace(status) != "fully-qualified" {
			continue
		}
		var seq []rune
		for _, field := range strings.Fields(codes) {
			cp, err := strconv.ParseUint(field, 16, 32)
			if err != nil {
				t.Fatalf("bad code point %q in %q", field, line)
			}
			seq = append(seq, rune(cp))
		}
		out = append(out, seq)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// resolvesToOneGlyph maps seq to glyphs through cmap, applies the GSUB
// ligatures longest first, and reports whether one non-.notdef glyph remains.
func resolvesToOneGlyph(t *testing.T, font *sfnt.Font, buf *sfnt.Buffer, ligs ligatures, seq []rune) bool {
	t.Helper()
	gids := make([]uint16, len(seq))
	for i, r := range seq {
		g, err := font.GlyphIndex(buf, r)
		if err != nil {
			t.Fatalf("GlyphIndex(%U): %v", r, err)
		}
		if g == 0 {
			return false
		}
		gids[i] = uint16(g)
	}
	var out []uint16
	for i := 0; i < len(gids); {
		n := 1
		glyph := gids[i]
		for l := len(gids) - i; l >= 2; l-- {
			if g, ok := ligs[seqKey(gids[i:i+l])]; ok {
				n, glyph = l, g
				break
			}
		}
		out = append(out, glyph)
		i += n
	}
	return len(out) == 1 && out[0] != 0
}
