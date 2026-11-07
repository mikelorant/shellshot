package emoji

import "testing"

// A cluster of only variation selectors has no rune left after stripping
// them; it must be reported missing, not panic.
func TestGlyphMapLoneVariationSelectors(t *testing.T) {
	for _, cluster := range []string{"\uFE0F", "\uFE0E", "\uFE0F\uFE0F"} {
		if _, ok := loadColor(t).Image(cluster, 16); ok {
			t.Errorf("Font drew %q", cluster)
		}
		if _, ok := loadBitmap(t).Image(cluster, 16); ok {
			t.Errorf("Bitmap drew %q", cluster)
		}
	}
}
