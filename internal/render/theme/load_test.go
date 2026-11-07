package theme

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func themeJSON(mod func(m map[string]string)) string {
	m := map[string]string{
		"name":       `"mine"`,
		"foreground": `"#010203"`,
		"background": `"#0A0b0C"`,
		"cursor":     `"#ffffff"`,
		"palette":    paletteJSON(16),
	}
	if mod != nil {
		mod(m)
	}
	var parts []string
	for k, v := range m {
		if v != "" {
			parts = append(parts, fmt.Sprintf("%q:%s", k, v))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func paletteJSON(n int) string {
	cs := make([]string, n)
	for i := range cs {
		cs[i] = fmt.Sprintf(`"#%02x0000"`, i)
	}
	return "[" + strings.Join(cs, ",") + "]"
}

func TestLoadValid(t *testing.T) {
	th, err := Load(strings.NewReader(themeJSON(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "mine" {
		t.Errorf("Name = %q", th.Name)
	}
	if want := (color.RGBA{1, 2, 3, 255}); th.Foreground != want {
		t.Errorf("Foreground = %v, want %v", th.Foreground, want)
	}
	if want := (color.RGBA{10, 11, 12, 255}); th.Background != want {
		t.Errorf("Background = %v, want %v", th.Background, want)
	}
	if want := (color.RGBA{255, 255, 255, 255}); th.Cursor != want {
		t.Errorf("Cursor = %v, want %v", th.Cursor, want)
	}
	if want := (color.RGBA{15, 0, 0, 255}); th.Palette[15] != want {
		t.Errorf("Palette[15] = %v, want %v", th.Palette[15], want)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", ``, "EOF"},
		{"not json", `nope`, "invalid"},
		{"trailing data", themeJSON(nil) + `{}`, "trailing"},
		{"unknown field", themeJSON(func(m map[string]string) { m["extra"] = "1" }), "unknown field"},
		{"missing name", themeJSON(func(m map[string]string) { m["name"] = "" }), "missing name"},
		{"missing foreground", themeJSON(func(m map[string]string) { m["foreground"] = "" }), "missing foreground"},
		{"missing background", themeJSON(func(m map[string]string) { m["background"] = "" }), "missing background"},
		{"missing cursor", themeJSON(func(m map[string]string) { m["cursor"] = "" }), "missing cursor"},
		{"bad hex digit", themeJSON(func(m map[string]string) { m["foreground"] = `"#12345g"` }), "foreground"},
		{"short hex", themeJSON(func(m map[string]string) { m["cursor"] = `"#fff"` }), "cursor"},
		{"no hash", themeJSON(func(m map[string]string) { m["background"] = `"010203"` }), "background"},
		{"palette too short", themeJSON(func(m map[string]string) { m["palette"] = paletteJSON(15) }), "15 colours"},
		{"palette too long", themeJSON(func(m map[string]string) { m["palette"] = paletteJSON(17) }), "17 colours"},
		{"palette missing", themeJSON(func(m map[string]string) { m["palette"] = "" }), "0 colours"},
		{"palette bad entry", themeJSON(func(m map[string]string) {
			m["palette"] = strings.Replace(paletteJSON(16), `"#030000"`, `"red"`, 1)
		}), "palette[3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tt.in))
			if !errors.Is(err, ErrTheme) {
				t.Fatalf("err = %v, want wrapping ErrTheme", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}
