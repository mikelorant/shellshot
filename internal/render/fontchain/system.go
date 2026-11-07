package fontchain

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// systemFamilies lists, in priority order, the font families --system-fonts
// looks for, as lower-case file name prefixes without spaces, dashes or
// underscores. They cover CJK text (Chinese, Japanese, Korean), then wide
// symbol coverage. Colour emoji fonts (Apple Color Emoji, Noto Color Emoji)
// are not listed: they are bitmap fonts the renderer cannot draw.
var systemFamilies = []string{
	"pingfang", "hiraginosansgb", "hiraginosans", "hiraginokakugothic", "applesdgothicneo", "stheiti", "songti",
	"notosansmonocjk", "notosanscjk", "notoserifcjk", "sourcehansans", "wenquanyi", "droidsansfallback",
	"arialunicode", "applesymbols", "unifont",
}

var fontExts = map[string]bool{".ttf": true, ".otf": true, ".ttc": true, ".otc": true}

// SystemDirs returns the directories searched for system fonts on this
// operating system.
func SystemDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{"/System/Library/Fonts", "/System/Library/Fonts/Supplemental", "/Library/Fonts", filepath.Join(home, "Library/Fonts")}
	case "linux":
		return []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(home, ".local/share/fonts"), filepath.Join(home, ".fonts")}
	}
	return nil
}

// SystemFonts returns the font files under dirs for each family of
// systemFamilies that is installed, in family priority order. For a family
// with several files it picks the regular weight (a file name containing
// "regular", else the shortest name), ties broken by path, so the choice does
// not depend on directory order. Unreadable directories are skipped.
func SystemFonts(dirs []string) []string {
	best := make(map[int]string) // family index -> chosen path
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !fontExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			name := normalise(strings.TrimSuffix(d.Name(), filepath.Ext(path)))
			for i, fam := range systemFamilies {
				if strings.HasPrefix(name, fam) {
					if cur, ok := best[i]; !ok || preferred(path, cur) {
						best[i] = path
					}
					break
				}
			}
			return nil
		})
	}
	idx := make([]int, 0, len(best))
	for i := range best {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	paths := make([]string, len(idx))
	for n, i := range idx {
		paths[n] = best[i]
	}
	return paths
}

// preferred reports whether font file a is a better pick than b for its
// family: "regular" in the name, then a shorter name, then the smaller path.
func preferred(a, b string) bool {
	ra, rb := isRegular(a), isRegular(b)
	if ra != rb {
		return ra
	}
	na, nb := filepath.Base(a), filepath.Base(b)
	if len(na) != len(nb) {
		return len(na) < len(nb)
	}
	return a < b
}

func isRegular(path string) bool {
	return strings.Contains(strings.ToLower(filepath.Base(path)), "regular")
}

// normalise lower-cases s and drops everything but letters and digits.
func normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
