package script

import "fmt"

// MaxDiagnostics is the number of errors reported; later ones are counted in
// Diagnostics.Dropped and not stored.
const MaxDiagnostics = 10

// Diagnostic is one error with its position. Hint, when set, is shown in
// parentheses after the message.
type Diagnostic struct {
	Pos  Pos
	Msg  string
	Hint string
}

// Error returns "line:col: message (hint)".
func (d Diagnostic) Error() string {
	s := fmt.Sprintf("%d:%d: %s", d.Pos.Line, d.Pos.Col, d.Msg)
	if d.Hint != "" {
		s += " (" + d.Hint + ")"
	}
	return s
}

// Format returns "file:line:col: message (hint)".
func (d Diagnostic) Format(file string) string {
	return file + ":" + d.Error()
}

// Diagnostics accumulates at most MaxDiagnostics Diagnostic values.
type Diagnostics struct {
	items   []Diagnostic
	dropped int
}

// Add records a diagnostic and reports whether it was stored (false once
// MaxDiagnostics are held).
func (ds *Diagnostics) Add(pos Pos, msg, hint string) bool {
	if len(ds.items) >= MaxDiagnostics {
		ds.dropped++
		return false
	}
	ds.items = append(ds.items, Diagnostic{Pos: pos, Msg: msg, Hint: hint})
	return true
}

// Items returns the stored diagnostics in the order they were added.
func (ds *Diagnostics) Items() []Diagnostic { return ds.items }

// Dropped is the number of diagnostics discarded after the limit was reached.
func (ds *Diagnostics) Dropped() int { return ds.dropped }

// maxSuggestDistance is the largest edit distance that still gives a
// did-you-mean suggestion.
const maxSuggestDistance = 2

// DidYouMean returns the hint text `did you mean "x"?` for the candidate
// closest to word (Levenshtein distance 1 to 2; the earliest candidate wins a
// tie), or "" when none is that close. A word that is itself a candidate gets
// no suggestion.
func DidYouMean(word string, candidates []string) string {
	best, bestDist := "", maxSuggestDistance+1
	for _, c := range candidates {
		d := EditDistance(word, c)
		if d == 0 {
			return ""
		}
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("did you mean %q?", best)
}

// EditDistance is the Levenshtein distance between a and b, in runes.
func EditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
