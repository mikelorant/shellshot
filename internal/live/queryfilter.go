package live

import (
	"bytes"
	"strings"
)

// maxHeldQuery is the most bytes of an unfinished escape sequence a
// QueryFilter holds back while it waits to see whether the sequence is a
// query. A longer unfinished sequence is passed on as it is.
const maxHeldQuery = 256

const (
	esc = 0x1b
	bel = 0x07
)

// QueryFilter removes from a stream of terminal output the sequences with
// which a program asks the terminal a question and expects an answer on its
// input: cursor position and status reports, device attributes, XTVERSION,
// mode reports, the kitty keyboard flags, window size reports, the colour
// scheme and the colour queries of OSC 4, 10, 11 and 12 (only with the
// payload "?"; the sequences that set a colour are kept). Everything else
// passes through unchanged. Sequences may be split across calls to Filter.
//
// It is used on the copy of the output shown on a real terminal, which would
// answer the queries with text on its own input. The zero value is ready to
// use. A QueryFilter is not safe for concurrent use.
type QueryFilter struct {
	held []byte // start of an unfinished escape sequence
	out  []byte
}

// Filter returns p without the query sequences. The result is valid until the
// next call and may be empty. The end of p can be held back when it begins an
// escape sequence that is not finished; Flush returns it.
func (f *QueryFilter) Filter(p []byte) []byte {
	data := p
	if len(f.held) > 0 {
		data = append(f.held, p...)
		f.held = nil
	}
	out := f.out[:0]
	for i := 0; i < len(data); {
		j := bytes.IndexByte(data[i:], esc)
		if j < 0 {
			out = append(out, data[i:]...)
			break
		}
		out = append(out, data[i:i+j]...)
		i += j
		n, state := classifyQuery(data[i:])
		switch {
		case state == isQuery:
			i += n
		case state == unfinished && len(data)-i <= maxHeldQuery:
			f.held = append([]byte(nil), data[i:]...)
			i = len(data)
		default:
			// Not a query, or too long to wait for: pass the ESC and look
			// for the next sequence after it.
			out = append(out, esc)
			i++
		}
	}
	f.out = out
	return out
}

// Flush returns what Filter holds back, which is an unfinished sequence, and
// forgets it. Call it when the output has ended.
func (f *QueryFilter) Flush() []byte {
	held := f.held
	f.held = nil
	return held
}

type queryState uint8

const (
	notQuery queryState = iota
	isQuery
	unfinished
)

// classifyQuery says whether b, which starts with ESC, begins with a query
// sequence, and if so how long it is.
func classifyQuery(b []byte) (n int, state queryState) {
	if len(b) < 2 {
		return 0, unfinished
	}
	switch b[1] {
	case '[':
		return classifyCSI(b)
	case ']':
		return classifyOSC(b)
	}
	return 0, notQuery
}

// csiQueries lists the CSI sequences (without ESC [) that ask for an answer.
var csiQueries = map[string]bool{
	"5n": true, "6n": true, "?6n": true, // status and cursor position
	"c": true, "0c": true, ">c": true, ">0c": true, "=c": true, "=0c": true, // DA1, DA2, DA3
	">q": true, ">0q": true, // XTVERSION
	"?996n": true,                                        // colour scheme
	"?u":    true,                                        // kitty keyboard flags
	"14t":   true, "16t": true, "18t": true, "19t": true, // size reports
}

func classifyCSI(b []byte) (int, queryState) {
	j := 2
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
		j++
	}
	if j == len(b) {
		return 0, unfinished
	}
	if b[j] < 0x40 || b[j] > 0x7e {
		return 0, notQuery
	}
	body := string(b[2 : j+1])
	if csiQueries[body] || isModeQuery(body) {
		return j + 1, isQuery
	}
	return 0, notQuery
}

// isModeQuery reports whether body is DECRQM: Ps $ p or ? Ps $ p.
func isModeQuery(body string) bool {
	mode, ok := strings.CutSuffix(body, "$p")
	return ok && isNumber(strings.TrimPrefix(mode, "?"))
}

func classifyOSC(b []byte) (int, queryState) {
	j := 2
	for j < len(b) && b[j] >= '0' && b[j] <= '9' {
		j++
	}
	if j == len(b) {
		return 0, unfinished
	}
	if b[j] != ';' || !queriesColour(string(b[2:j])) {
		return 0, notQuery
	}
	// Find the terminator: BEL or ESC \.
	k := j
	for k < len(b) && b[k] != bel && b[k] != esc {
		k++
	}
	if k == len(b) || (b[k] == esc && k+1 == len(b)) {
		return 0, unfinished
	}
	end := k + 1
	if b[k] == esc {
		if b[k+1] != '\\' {
			return 0, notQuery
		}
		end = k + 2
	}
	if isColourQuery(string(b[2:j]), string(b[j+1:k])) {
		return end, isQuery
	}
	return 0, notQuery
}

// queriesColour reports whether the OSC command ps can ask for a colour.
func queriesColour(ps string) bool {
	switch ps {
	case "4", "10", "11", "12":
		return true
	}
	return false
}

// isColourQuery reports whether the OSC command ps with payload asks for a
// colour: "?" for the dynamic colours, "N;?" (repeated) for the palette.
func isColourQuery(ps, payload string) bool {
	if ps != "4" {
		return payload == "?"
	}
	parts := strings.Split(payload, ";")
	if len(parts)%2 != 0 {
		return false
	}
	for i := 0; i < len(parts); i += 2 {
		if !isNumber(parts[i]) || parts[i+1] != "?" {
			return false
		}
	}
	return true
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
