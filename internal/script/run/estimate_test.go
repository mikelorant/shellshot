package run

import (
	"reflect"
	"testing"
	"time"
)

// The exit sequence adds, to every plan, a final drain (min: the settle quiet
// time, max: 2s) and an exit (max: 2s grace). With settle at its default of
// 200ms that is exitMin 200ms and exitMax 4s.
const (
	exitMin = 200 * time.Millisecond
	exitMax = 4 * time.Second
)

func TestEstimate(t *testing.T) {
	ms, s := time.Millisecond, time.Second
	tests := []struct {
		name     string
		src      string
		min, max time.Duration
		waits    int
	}{
		{
			name: "empty plan is the exit sequence only",
			src:  "",
			min:  exitMin, max: exitMax,
		},
		{
			// "abc": 2 delays of 10ms; Enter: 1 delay of 10ms; settle: quiet
			// 200ms at least, timeout 15s at most.
			name: "run",
			src:  "typing 10ms\nrun abc\n",
			min:  (20+10)*ms + 200*ms + exitMin, max: (20+10)*ms + 15*s + exitMax,
		},
		{
			// 3 delays of 100ms*(1+-50%): 150ms to 450ms.
			name: "jitter",
			src:  "typing 100ms\njitter 50%\ntype abcd\n",
			min:  150*ms + exitMin, max: 450*ms + exitMax,
		},
		{
			// "héé" is 3 runes, so 2 delays.
			name: "runes not bytes",
			src:  "typing 10ms\ntype héé\n",
			min:  20*ms + exitMin, max: 20*ms + exitMax,
		},
		{
			// A text block is one stream: "a", Enter, "b", Enter, "c" is 5
			// units, 4 delays, then one linger.
			name: "text block",
			src:  "typing 10ms\nlinger 1s\ntype lines {\n  a\n  b\n  c\n}\n",
			min:  40*ms + s + exitMin, max: 40*ms + s + exitMax,
		},
		{
			// up x3: 2 delays; enter after a gap: 1 delay; one linger.
			name: "press",
			src:  "typing 20ms\nlinger 100ms\npress up x3 enter\n",
			min:  60*ms + 100*ms + exitMin, max: 60*ms + 100*ms + exitMax,
		},
		{
			name: "settle off",
			src:  "settle off\nrun a\n",
			// Enter after a gap: 50ms. The drain still uses 200ms.
			min: 50*ms + exitMin, max: 50*ms + exitMax,
		},
		{
			// sleep 2s has no linger; the wait is 0 to 5s plus a linger; the
			// assertion takes no time but lingers; type "q" has no delay and
			// lingers.
			name: "sleep linger waits",
			src:  "linger 1s\nsleep 2s\nwait \"x\" 5s\nwait \"y\" 0s\ntype q\n",
			min:  (2+1+1+1)*s + exitMin, max: (2+5+1+1+1)*s + exitMax,
			waits: 1,
		},
		{
			name: "wait idle",
			src:  "settle 300ms\ntimeout 10s\nwait idle\n",
			// The exit drain quiets for 300ms here.
			min: 300*ms + 300*ms, max: 10*s + exitMax,
			waits: 1,
		},
		{
			name: "mark and hint add nothing",
			src:  "speed 2x\nscene a {\n  mark m\n}\n",
			min:  exitMin, max: exitMax,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := lowerSrc(t, tt.src).Estimate()
			if e.Min != tt.min || e.Max != tt.max || e.Waits != tt.waits {
				t.Errorf("estimate = min %v max %v waits %d, want min %v max %v waits %d",
					e.Min, e.Max, e.Waits, tt.min, tt.max, tt.waits)
			}
		})
	}
}

func TestEstimateScenes(t *testing.T) {
	src := `scene a {
  hidden {
    sleep 1s
  }
  sleep 2s
}
hidden {
  sleep 4s
}
scene b {
  wait "x" 3s
}
`
	e := lowerSrc(t, src).Estimate()
	want := []SceneEstimate{
		{ID: 1, Name: "a", Min: 3 * time.Second, Max: 3 * time.Second}, // the hidden block inside counts
		{ID: 4, Name: "b", Min: 0, Max: 3 * time.Second},
	}
	if !reflect.DeepEqual(e.Scenes, want) {
		t.Errorf("scenes\n got %+v\nwant %+v", e.Scenes, want)
	}
	// The top-level hidden block counts only in the total.
	if e.Min != 7*time.Second+exitMin || e.Max != 10*time.Second+exitMax {
		t.Errorf("total = %v to %v", e.Min, e.Max)
	}
}
