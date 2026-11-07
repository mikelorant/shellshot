package emoji

import (
	"strings"
	"testing"
)

func TestLoadDetectsFontFormat(t *testing.T) {
	b, err := Load(buildColorFont(t))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := b.(*Font); !ok {
		t.Errorf("COLR font loaded as %T, want *Font", b)
	} else if _, drawn := f.Image(smiley, 16); !drawn {
		t.Error("COLR backend drew nothing")
	}

	b, err = Load(buildBitmapFont(t, testEmojiStrikes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if bm, ok := b.(*Bitmap); !ok {
		t.Errorf("bitmap font loaded as %T, want *Bitmap", b)
	} else if _, drawn := bm.Image(manWoman, 16); !drawn {
		t.Error("bitmap backend drew nothing")
	}
}

func TestLoadRejectsFontsWithoutColourTables(t *testing.T) {
	for name, data := range map[string][]byte{
		"monochrome font": readAsset(t, "jetbrains-mono/JetBrainsMono-Regular.ttf"),
		"empty":           nil,
		"garbage":         []byte("not a font at all, not even close"),
	} {
		if _, err := Load(data); err == nil {
			t.Errorf("%s: Load succeeded", name)
		}
	}
	_, err := Load(readAsset(t, "jetbrains-mono/JetBrainsMono-Regular.ttf"))
	if err == nil || !strings.Contains(err.Error(), "no colour glyph tables (need COLR/CPAL or CBLC/CBDT)") {
		t.Errorf("monochrome font: err = %v, want the missing colour tables message", err)
	}
}

func TestLoadPropagatesBackendErrors(t *testing.T) {
	good := buildBitmapFont(t, testEmojiStrikes(t))
	if _, err := Load(good[:len(good)-10]); err == nil {
		t.Error("truncated bitmap font loaded")
	}
	colr := buildColorFont(t)
	if _, err := Load(colr[:len(colr)-40]); err == nil {
		t.Error("truncated COLR font loaded")
	}
}
