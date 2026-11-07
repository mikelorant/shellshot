package svg

import (
	"strconv"
	"strings"
	"time"
)

// clock maps the steps' start times to keyframe percentages of the whole
// animation.
type clock struct {
	starts []time.Duration
	total  time.Duration
	loops  int
	digits int // decimals of a percentage: enough for 1 ms
}

func newClock(starts []time.Duration, total time.Duration, loops int) clock {
	digits, limit := 2, int64(10000) // 1 ms is 100/total % of the animation
	for total.Milliseconds() > limit && digits < 6 {
		digits++
		limit *= 10
	}
	return clock{starts: starts, total: total, loops: loops, digits: digits}
}

// percent formats the start of step i as a keyframe selector.
func (c clock) percent(i int) string {
	v := float64(c.starts[i]) / float64(c.total) * 100
	s := strconv.FormatFloat(v, 'f', c.digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + "%"
}

// seconds formats the whole duration for the animation shorthand.
func (c clock) seconds() string {
	s := strconv.FormatFloat(c.total.Seconds(), 'f', 3, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".") + "s"
}

// frames returns the keyframes for a property that is state[i] at step i:
// one for each change of state, where decl turns a state into declarations.
// A finite animation gets a final 100% keyframe so that its last frame stays
// (animation-fill-mode: forwards shows the 100% keyframe, not the last one
// written).
func (c clock) frames(state []int, decl func(int) string) []keyframe {
	var out []keyframe
	for i, s := range state {
		if i > 0 && s == state[i-1] {
			continue
		}
		kf := keyframe{c.percent(i), decl(s)}
		if n := len(out); n > 0 && out[n-1].selector == kf.selector {
			out[n-1] = kf // steps closer than the precision share a keyframe
			continue
		}
		out = append(out, kf)
	}
	if c.loops > 0 {
		out = append(out, keyframe{"100%", decl(state[len(state)-1])})
	}
	return out
}

// keyframe is one keyframe: a percentage selector and its declarations.
type keyframe struct{ selector, declarations string }

// animations collects the keyframe lists of a document. Identical lists share
// one @keyframes rule and class.
type animations struct {
	clock  clock
	bodies []string
	index  map[string]string // keyframes -> class
}

// add registers frames and returns the class that plays them.
func (a *animations) add(frames []keyframe) string {
	var sb strings.Builder
	for _, f := range frames {
		sb.WriteString(f.selector + "{" + f.declarations + "}")
	}
	body := sb.String()
	if class, ok := a.index[body]; ok {
		return class
	}
	if a.index == nil {
		a.index = map[string]string{}
	}
	class := "a" + strconv.Itoa(len(a.bodies))
	a.index[body] = class
	a.bodies = append(a.bodies, body)
	return class
}

// css returns the style sheet: for each list a rule that plays it over the
// whole duration with no easing between keyframes, and the keyframes.
func (a *animations) css() string {
	if len(a.bodies) == 0 {
		return ""
	}
	iter := "infinite"
	if a.clock.loops > 0 {
		iter = strconv.Itoa(a.clock.loops) + " forwards"
	}
	var sb strings.Builder
	for i := range a.bodies {
		name := "a" + strconv.Itoa(i)
		sb.WriteString("." + name + "{animation:" + name + " " + a.clock.seconds() + " steps(1,end) " + iter + "}")
	}
	for i, body := range a.bodies {
		sb.WriteString("@keyframes a" + strconv.Itoa(i) + "{" + body + "}")
	}
	return sb.String()
}
