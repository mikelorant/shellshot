package live

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryFilter(t *testing.T) {
	const st = "\x1b\\"
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"plain text", []string{"hello\r\nworld"}, "hello\r\nworld"},
		{"cursor position", []string{"a\x1b[6nb"}, "ab"},
		{"status report", []string{"a\x1b[5nb"}, "ab"},
		{"private cursor position", []string{"a\x1b[?6nb"}, "ab"},
		{"device attributes 1", []string{"\x1b[c\x1b[0c"}, ""},
		{"device attributes 2 and 3", []string{"\x1b[>c\x1b[>0c\x1b[=c\x1b[=0c"}, ""},
		{"xtversion", []string{"a\x1b[>0qb\x1b[>qc"}, "abc"},
		{"colour scheme", []string{"\x1b[?996n"}, ""},
		{"kitty keyboard flags", []string{"\x1b[?u"}, ""},
		{"size reports", []string{"\x1b[14t\x1b[16t\x1b[18t\x1b[19t"}, ""},
		{"mode report", []string{"a\x1b[?2004$pb\x1b[4$pc"}, "abc"},
		{"background ST", []string{"x\x1b]11;?" + st + "y"}, "xy"},
		{"background BEL", []string{"x\x1b]11;?\ay"}, "xy"},
		{"foreground and cursor", []string{"\x1b]10;?" + st + "\x1b]12;?\a"}, ""},
		{"palette", []string{"\x1b]4;1;?" + st + "\x1b]4;1;?;2;?\a"}, ""},
		{"two queries in a row", []string{"\x1b]11;?" + st + "\x1b[6n"}, ""},
		{"escape before a query", []string{"\x1b\x1b[6n"}, "\x1b"},

		{"background setter", []string{"\x1b]11;rgb:ff/00/00" + st}, "\x1b]11;rgb:ff/00/00" + st},
		{"palette setter", []string{"\x1b]4;1;#ff0000\a"}, "\x1b]4;1;#ff0000\a"},
		{"palette mixed", []string{"\x1b]4;1;?;2;#00ff00\a"}, "\x1b]4;1;?;2;#00ff00\a"},
		{"title", []string{"\x1b]0;?\a"}, "\x1b]0;?\a"},
		{"reset colour", []string{"\x1b]111\a\x1b]104;1\a"}, "\x1b]111\a\x1b]104;1\a"},
		{"cursor position truncated by another final", []string{"\x1b[6x"}, "\x1b[6x"},
		{"cursor move", []string{"\x1b[6;1H"}, "\x1b[6;1H"},
		{"sgr", []string{"\x1b[1;31mred\x1b[0m"}, "\x1b[1;31mred\x1b[0m"},
		{"cursor position report", []string{"\x1b[12;4R"}, "\x1b[12;4R"},
		{"kitty keyboard set", []string{"\x1b[>1u\x1b[=1;1u"}, "\x1b[>1u\x1b[=1;1u"},
		{"other mode query-like", []string{"\x1b[?2004h\x1b[?2004l"}, "\x1b[?2004h\x1b[?2004l"},
		{"mode report without number", []string{"\x1b[$p"}, "\x1b[$p"},
		{"esc followed by other", []string{"\x1b7\x1b8\x1bM"}, "\x1b7\x1b8\x1bM"},
		{"osc interrupted by escape", []string{"\x1b]11;?\x1b[6n"}, "\x1b]11;?"},

		{"split inside CSI", []string{"a\x1b[", "6", "nb"}, "ab"},
		{"split after ESC", []string{"a\x1b", "[6nb"}, "ab"},
		{"split inside OSC", []string{"a\x1b]11;", "?\x1b", "\\b"}, "ab"},
		{"split before the terminator", []string{"\x1b]11;?", "\a"}, ""},
		{"split non-query CSI", []string{"a\x1b[3", "1mb"}, "a\x1b[31mb"},
		{"split setter", []string{"\x1b]11;rgb:", "ff/ff/ff\x1b", "\\"}, "\x1b]11;rgb:ff/ff/ff\x1b\\"},
		{"split byte by byte", strings.Split("a\x1b]11;?\x1b\\b\x1b[6nc\x1b[31md", ""), "ab" + "c\x1b[31md"},
		{"query after held non-query", []string{"\x1b[3", "1m\x1b[6n"}, "\x1b[31m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f QueryFilter
			var got bytes.Buffer
			for _, c := range tc.chunks {
				got.Write(f.Filter([]byte(c)))
			}
			got.Write(f.Flush())
			if got.String() != tc.want {
				t.Errorf("output = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestQueryFilterHoldsAnUnfinishedSequence(t *testing.T) {
	var f QueryFilter
	if got := f.Filter([]byte("ab\x1b[6")); string(got) != "ab" {
		t.Errorf("Filter = %q, want ab", got)
	}
	if got := f.Flush(); string(got) != "\x1b[6" {
		t.Errorf("Flush = %q, want the held sequence", got)
	}
	if got := f.Flush(); len(got) != 0 {
		t.Errorf("second Flush = %q, want nothing", got)
	}
}

func TestQueryFilterDoesNotHoldMoreThanTheLimit(t *testing.T) {
	var f QueryFilter
	long := "\x1b]0;" + strings.Repeat("t", 2*maxHeldQuery)
	// A title is recognised as not a query at once.
	if got := f.Filter([]byte(long)); string(got) != long {
		t.Errorf("long title: output length %d, want %d", len(got), len(long))
	}
	// An endless query parameter list is passed on once it is too long.
	long = "\x1b[" + strings.Repeat("1;", 2*maxHeldQuery)
	got := f.Filter([]byte(long))
	if len(got)+len(f.held) != len(long) || len(f.held) > maxHeldQuery {
		t.Errorf("output %d + held %d bytes, want %d in total with at most %d held", len(got), len(f.held), len(long), maxHeldQuery)
	}
	if !strings.HasPrefix(long, string(got)) || len(got) == 0 {
		t.Errorf("long unfinished CSI was not passed on")
	}
}

func TestTeeHidingQueries(t *testing.T) {
	var out bytes.Buffer
	in := "hi \x1b]11;?\x1b\\\x1b[6n there"
	p := TeeHidingQueries(fakeProcess{Reader: strings.NewReader(in)}, &out)
	buf := make([]byte, 64)
	n, err := p.Read(buf)
	if err != nil || string(buf[:n]) != in {
		t.Fatalf("Read = %q, %v; want the unfiltered input", buf[:n], err)
	}
	if out.String() != "hi  there" {
		t.Errorf("out = %q, want %q", out.String(), "hi  there")
	}
}

func TestTeeHidingQueriesFlushesAtEOF(t *testing.T) {
	var out bytes.Buffer
	p := TeeHidingQueries(fakeProcess{Reader: strings.NewReader("end\x1b[")}, &out)
	buf := make([]byte, 64)
	for {
		if _, err := p.Read(buf); err != nil {
			break
		}
	}
	if out.String() != "end\x1b[" {
		t.Errorf("out = %q, want the unfinished sequence shown at EOF", out.String())
	}
}
