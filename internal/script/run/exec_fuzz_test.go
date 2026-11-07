package run

import (
	"testing"
	"time"
)

var fuzzScreens = []string{
	"", "$", "$ ", "$ echo hi\nhi\n$", "~\n~", "BUILD-STATUS=0", "app v1.2.3", "Usage: app", "a1b2c3d initial commit",
}

// FuzzExecutor drives the Executor over the example plans with random Poll
// times, screens and a child exit, and checks that it never panics, never
// returns a Deadline before now, stays Done or failed once it is, and
// reaches Done or an error when polled at its deadlines.
func FuzzExecutor(f *testing.F) {
	plans := make([]*Plan, 5)
	for i, name := range []string{"ex1_hello", "ex2_git_scenes", "ex3_vim_tui", "ex4_long_build", "ex5_ci_smoke"} {
		plans[i] = lowerSrc(f, readScript(f, "testdata/lower/"+name+".script"))
	}
	f.Add(uint8(0), []byte{1, 2, 3, 4, 5, 6})
	f.Add(uint8(4), []byte{200, 0, 7, 9, 255, 255, 3, 3, 3})
	f.Add(uint8(3), []byte{0, 0, 0, 0, 0, 0, 128, 1, 2})
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		ex, err := New(plans[int(which)%len(plans)], Deps{Keys: fakeKeys{}})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Duration(0)
		screen := textScreen("")
		check := func(step Step, err error) bool {
			if err == nil && step.Deadline < now {
				t.Fatalf("Deadline %v before now %v", step.Deadline, now)
			}
			return err != nil || step.Done
		}
		finished := false
		for i := 0; i+2 < len(data) && !finished; i += 3 {
			switch b := data[i]; {
			case b >= 0xf0:
				ex.ChildExited(now, int(data[i+1]))
			case b >= 0xc0:
				now -= time.Duration(data[i+1]) * time.Millisecond // time must never go backwards inside the Executor
				now = max(now, 0)
			default:
				now += time.Duration(b) * time.Duration(data[i+1]) * time.Millisecond
			}
			screen = textScreen(fuzzScreens[int(data[i+2])%len(fuzzScreens)])
			finished = check(ex.Poll(now, screen))
		}
		idle := 0 // polls in a row that did nothing and asked to be polled again at once
		for polls := 0; !finished; polls++ {
			if polls > 200000 {
				t.Fatal("the script did not finish")
			}
			step, err := ex.Poll(now, screen)
			finished = check(step, err)
			if step.Write == nil && step.Mark == "" && step.Span == nil && !step.Shutdown && step.Deadline <= now {
				if idle++; idle > 3 && !finished {
					t.Fatalf("busy loop at %v", now)
				}
			} else {
				idle = 0
			}
			now = max(now, step.Deadline)
		}
		// Done and failed are final.
		s1, e1 := ex.Poll(now+time.Hour, screen)
		s2, e2 := ex.Poll(now+2*time.Hour, screen)
		if e1 != e2 || (e1 == nil && (!s1.Done || !s2.Done || s1.Write != nil || s1.Span != nil || s1.Mark != "")) {
			t.Fatalf("not final: %+v %v / %+v %v", s1, e1, s2, e2)
		}
	})
}
