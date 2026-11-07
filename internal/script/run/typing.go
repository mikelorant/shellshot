package run

import (
	"math"
	"time"
	"unicode/utf8"
)

// Typing and pressing are paced the same way: a step is a stream of units, a
// unit is sent per Poll at its scheduled time, and the unit after it is due
// one jittered typing delay later, counted from when the unit was sent (so a
// late Poll delays the rest of the stream instead of making it burst). A
// delay of 0 sends the whole step in one Write.

// delay returns one typing delay: d spread uniformly over d*(1±jitter%).
// Nothing is drawn from the random source when the result is fixed, so
// changing jitter from 0 does not shift later draws of other steps.
func (e *Executor) delay(d time.Duration, jitter float64) time.Duration {
	if d <= 0 {
		return 0
	}
	if jitter <= 0 {
		return d
	}
	spread := jitter / 100 * (2*e.rng.Float64() - 1)
	return time.Duration(math.Round(float64(d) * (1 + spread)))
}

// typeUnits splits text into the units typed one at a time: a rune, with
// "\r\n" as one unit (one Enter). Invalid UTF-8 bytes stay as they are, for
// the Keys adapter to reject.
func typeUnits(text string) []string {
	var units []string
	for i := 0; i < len(text); {
		n := 1
		switch {
		case text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n':
			n = 2
		case text[i] >= 0x80:
			_, n = utf8.DecodeRuneInString(text[i:])
		}
		units = append(units, text[i:i+n])
		i += n
	}
	return units
}

// advanceType types op.Text. cur.units are the units still to send (all at
// once when PerChar is 0), cur.i the number sent and cur.next when the next
// is due.
func (e *Executor) advanceType(op OpType, now time.Duration) (Step, bool, *ScriptError) {
	s := &e.cur
	if s.units == nil {
		if op.Text == "" {
			return Step{}, true, nil
		}
		if op.PerChar <= 0 {
			s.units = []string{op.Text}
		} else {
			s.units = typeUnits(op.Text)
		}
		s.n, s.next = len(s.units), s.start
	}
	if e.exited {
		return Step{}, false, e.fail(op.Meta, ChildExited, "child exited (status %d) before the script could type %q", e.exitStatus, shorten(op.Text))
	}
	if now < s.next {
		return Step{Deadline: s.next}, false, nil
	}
	b, err := e.keys.EncodeText(s.units[s.i])
	if err != nil {
		return Step{}, false, e.fail(op.Meta, KeyEncode, "cannot type %q: %v", s.units[s.i], err)
	}
	s.i++
	if s.i == s.n {
		return Step{Write: b}, true, nil
	}
	s.next = now + e.delay(op.PerChar, op.Jitter)
	return Step{Write: b, Deadline: s.next}, false, nil
}

// advancePress presses op.Chord op.Repeat times. cur.i is the presses sent,
// cur.next when the next is due; the first is due one delay after the start
// when op.Gap says it follows another unit of the same step. With a typing
// delay of 0 all presses go out in one Write.
func (e *Executor) advancePress(op OpPress, now time.Duration) (Step, bool, *ScriptError) {
	s := &e.cur
	if !s.flag {
		s.flag = true
		s.n = max(op.Repeat, 1)
		s.next = s.start
		if op.Gap {
			s.next += e.delay(op.Eff.Typing, op.Eff.Jitter)
		}
	}
	if e.exited {
		return Step{}, false, e.fail(op.Meta, ChildExited, "child exited (status %d) before the script could press %s", e.exitStatus, op.Chord)
	}
	if now < s.next {
		return Step{Deadline: s.next}, false, nil
	}
	var out []byte
	for {
		b, err := e.keys.Encode(op.Chord)
		if err != nil {
			return Step{}, false, e.fail(op.Meta, KeyEncode, "cannot press %s: %v", op.Chord, err)
		}
		out = append(out, b...)
		s.i++
		if s.i == s.n {
			return Step{Write: out}, true, nil
		}
		if op.Eff.Typing > 0 {
			break
		}
	}
	s.next = now + e.delay(op.Eff.Typing, op.Eff.Jitter)
	return Step{Write: out, Deadline: s.next}, false, nil
}

// shorten cuts s to 40 runes for messages.
func shorten(s string) string {
	const limit = 40
	n := 0
	for i := range s {
		if n == limit {
			return s[:i] + "..."
		}
		n++
	}
	return s
}
