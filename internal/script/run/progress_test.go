package run

import (
	"reflect"
	"testing"
)

func TestPlanStepsCountsSourceSteps(t *testing.T) {
	tests := []struct {
		name, src string
		want      int
	}{
		{"empty script", "", 0},
		{"run is one step", "run echo hi\n", 1},
		{"press with several keys is one step", "press up up enter\n", 1},
		{"linger does not add steps", "linger 1s\nrun a\nwait \"x\"\n", 2},
		{"text block is one step", "type lines {\n  a\n  b\n}\n", 1},
		{"run block is one step", "run lines {\n  a\n  b\n}\n", 1},
		{"blocks are not steps", "scene a {\n  mark m\n}\nhidden {\n  sleep 1s\n}\n", 2},
		{"empty block has no steps", "scene a {\n}\n", 0},
		{"steps in nested hidden block count", "scene a {\n  hidden {\n    run x\n    type y\n  }\n  sleep 1s\n}\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerSrc(t, tt.src).Steps(); got != tt.want {
				t.Errorf("Steps() = %d, want %d", got, tt.want)
			}
		})
	}
}

// progressOf runs src on the fake shell and returns what OnProgress saw and
// the error of the run.
func progressOf(t *testing.T, src string) ([]Progress, error) {
	t.Helper()
	var got []Progress
	_, err := runWorld(t, lowerSrc(t, src), newFakeShell(), Deps{OnProgress: func(p Progress) { got = append(got, p) }})
	return got, err
}

func TestOnProgressReportsEachCompletedStepInOrder(t *testing.T) {
	src := `mark a
scene one {
  run echo hi
  hidden {
    type x
    press enter
  }
  type lines {
    a
    b
  }
}
sleep 1s
`
	got, err := progressOf(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Progress{
		{0, 6, ""},
		{1, 6, "one"},
		{2, 6, "one/hidden"},
		{3, 6, "one/hidden"},
		{4, 6, "one/hidden"},
		{4, 6, "one"},
		{5, 6, "one"},
		{5, 6, ""},
		{6, 6, ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("progress =\n%v\nwant\n%v", got, want)
	}
}

func TestOnProgressStopsBeforeTheFailingStep(t *testing.T) {
	got, err := progressOf(t, "scene check {\n  run echo hi\n  wait /^nope$/ 0s\n}\n")
	if err == nil {
		t.Fatal("run succeeded, want the failed wait")
	}
	want := []Progress{{0, 2, "check"}, {1, 2, "check"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("progress = %v, want %v", got, want)
	}
}

func TestOnProgressDoesNotChangeTheTimeline(t *testing.T) {
	plan := lowerSrc(t, "scene s {\n  run echo hi\n  hidden {\n    sleep 1s\n  }\n}\n")
	without, err := runWorld(t, plan, newFakeShell(), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := runWorld(t, plan, newFakeShell(), Deps{OnProgress: func(Progress) {}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(with, without) {
		t.Errorf("timeline differs with OnProgress:\n%s\nwithout:\n%s", timelineText(with), timelineText(without))
	}
}

func TestOnProgressOfAnEmptyScriptHasNoSteps(t *testing.T) {
	got, err := progressOf(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Progress{{0, 0, ""}}; !reflect.DeepEqual(got, want) {
		t.Errorf("progress = %v, want %v", got, want)
	}
}
