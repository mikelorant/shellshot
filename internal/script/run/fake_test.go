package run

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script/keys"
)

// fakeKeys encodes the keys the tests use. A chord or text containing
// failOn gives an error, to test the KeyEncode failure.
type fakeKeys struct{ failOn string }

func (k fakeKeys) Encode(c keys.Chord) ([]byte, error) {
	s := c.String()
	if k.failOn != "" && s == k.failOn {
		return nil, fmt.Errorf("fake keys: cannot encode %s", s)
	}
	table := map[string]string{
		"enter": "\r", "up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
		"esc": "\x1b", "tab": "\t", "space": " ", "ctrl+c": "\x03", "ctrl+d": "\x04",
	}
	if b, ok := table[s]; ok {
		return []byte(b), nil
	}
	if len(s) == 1 {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("fake keys: no encoding for %s", s)
}

func (k fakeKeys) EncodeText(s string) ([]byte, error) {
	if k.failOn != "" && strings.Contains(s, k.failOn) {
		return nil, fmt.Errorf("fake keys: cannot type %q", s)
	}
	s = strings.ReplaceAll(s, "\r\n", "\r")
	return []byte(strings.ReplaceAll(s, "\n", "\r")), nil
}

// Event is one thing the host did, for the timeline goldens.
type Event struct {
	At   time.Duration
	Kind string
	Data string
}

// String renders the event in the notation of the timeline goldens:
// "t=+50ms write "h"".
func (e Event) String() string {
	s := fmt.Sprintf("t=+%v %s", e.At, e.Kind)
	if e.Data != "" {
		s += " " + e.Data
	}
	return s
}

func timelineText(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// world is what the fake clock loop drives: a screen that reacts to writes
// and changes by itself at scheduled times.
type world interface {
	Screen
	// Write delivers bytes the Executor sent at now.
	Write(now time.Duration, b []byte)
	// Shutdown delivers the end-of-script shutdown request at now.
	Shutdown(now time.Duration)
	// Advance applies the changes due at or before now.
	Advance(now time.Duration)
	// Next is the time of the next scheduled change.
	Next() (time.Duration, bool)
	// TakeExit reports a child exit that has not been reported yet.
	TakeExit() (status int, ok bool)
}

// driver runs an Executor on a fake clock: it polls at the deadlines and
// whenever the world changes, as the session loop does.
type driver struct {
	ex     *Executor
	w      world
	events []Event
	now    time.Duration
	// polls counts the Poll calls; deadlines holds the Deadline of each.
	polls     int
	deadlines []time.Duration
}

const (
	maxSimTime = 30 * time.Minute
	maxPolls   = 200000
)

// runWorld drives the plan to Done or to an error and returns the timeline and
// the error. It fails the test on a busy loop or a Deadline in the past.
func runWorld(t testing.TB, p *Plan, w world, deps Deps) ([]Event, error) {
	t.Helper()
	if deps.Keys == nil {
		deps.Keys = fakeKeys{}
	}
	ex, err := New(p, deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d := &driver{ex: ex, w: w}
	return d.run(t)
}

func (d *driver) record(kind, data string) {
	d.events = append(d.events, Event{At: d.now, Kind: kind, Data: data})
}

func (d *driver) run(t testing.TB) ([]Event, error) {
	t.Helper()
	stuck := 0
	for ; d.polls < maxPolls && d.now <= maxSimTime; d.polls++ {
		d.w.Advance(d.now)
		if status, ok := d.w.TakeExit(); ok {
			d.ex.ChildExited(d.now, status)
			d.record("child-exit", strconv.Itoa(status))
		}
		step, err := d.ex.Poll(d.now, d.w)
		d.deadlines = append(d.deadlines, step.Deadline)
		if step.Deadline < d.now {
			t.Fatalf("t=%v: Deadline %v is in the past", d.now, step.Deadline)
		}
		if err != nil {
			d.record("error", err.Error())
			return d.events, err
		}
		produced := d.apply(step)
		if step.Done {
			d.record("done", "")
			return d.events, nil
		}
		if step.Deadline > d.now {
			next := step.Deadline
			if at, ok := d.w.Next(); ok && at < next {
				next = max(at, d.now)
			}
			d.now = next
			stuck = 0
			continue
		}
		if !produced {
			if stuck++; stuck > 2 {
				t.Fatalf("t=%v: busy loop: Poll returned Deadline == now with nothing to do", d.now)
			}
		}
	}
	t.Fatalf("script did not finish after %d polls at t=%v", d.polls, d.now)
	return nil, nil
}

// apply records and performs one Step and reports whether it did anything.
func (d *driver) apply(s Step) bool {
	did := false
	if s.Write != nil {
		d.record("write", strconv.Quote(string(s.Write)))
		d.w.Write(d.now, s.Write)
		did = true
	}
	if s.Mark != "" {
		d.record("mark", s.Mark)
		did = true
	}
	if s.Span != nil {
		ev := s.Span.Event
		if ev.Op == 1 {
			name := "hidden"
			if ev.Type == event.SpanScene {
				name = "scene:" + ev.Name
			} else if ev.Name != "" {
				name = "hidden:" + ev.Name
			}
			d.record("span-begin", fmt.Sprintf("%s id=%d parent=%d%s", name, ev.ID, ev.Parent, hintText(s.Span)))
		} else {
			d.record("span-end", fmt.Sprintf("id=%d", ev.ID))
		}
		did = true
	}
	if s.Shutdown {
		d.record("shutdown", "")
		d.w.Shutdown(d.now)
		did = true
	}
	return did
}

func hintText(s *SpanOp) string {
	h := s.Event.Hints
	var out string
	if h.Speed != 0 {
		out += fmt.Sprintf(" speed=%d", h.Speed)
	}
	if h.Idle != 0 {
		out += " idle=" + h.Idle.String()
	}
	if h.Hold != 0 {
		out += " hold=" + h.Hold.String()
	}
	return out
}

// pending is a change the fake world will apply at a time.
type pending struct {
	at     time.Duration
	text   string
	clear  bool
	mode   string
	exit   *int
	cancel bool // dropped by Ctrl-C
}

// fakeShell is a tiny terminal plus shell that echoes typed text, runs a few
// canned commands after a latency and prints their output. Its screen is the
// text so far, last 24 rows.
type fakeShell struct {
	buf     string
	line    string
	hist    []string
	histPos int
	mode    string // "" shell, "vim"
	pend    []pending
	exit    *int
	taken   bool
	latency time.Duration
}

func newFakeShell() *fakeShell { return &fakeShell{buf: "$ ", latency: 20 * time.Millisecond} }

func (s *fakeShell) Text() string {
	lines := strings.Split(s.buf, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	if len(lines) > 24 {
		lines = lines[len(lines)-24:]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// ShellForeground: the shell is in the foreground when no command is still
// to print its output, and it is not inside vim.
func (s *fakeShell) ShellForeground() (bool, error) { return len(s.pend) == 0 && s.mode == "", nil }

func (s *fakeShell) Write(now time.Duration, b []byte) {
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == 0x1b:
			if i+2 < len(b) && b[i+1] == '[' {
				if b[i+2] == 'A' {
					s.recall()
				}
				i += 2
			}
		case c == 0x03:
			kept := s.pend[:0]
			for _, p := range s.pend {
				if !p.cancel {
					kept = append(kept, p)
				}
			}
			s.pend = kept
			s.buf += "^C\n$ "
			s.line = ""
		case c == 0x04:
			s.queue(now, pending{at: s.latency / 2, exit: new(int)})
		case c == '\r':
			cmd := strings.TrimSpace(s.line)
			s.line = ""
			if s.mode == "vim" {
				if strings.HasSuffix(cmd, ":wq") {
					s.mode = ""
					s.queue(now, pending{at: s.latency, clear: true, text: "$ "})
				}
				continue
			}
			s.buf += "\n"
			if cmd != "" {
				s.hist = append(s.hist, cmd)
			}
			s.histPos = len(s.hist)
			s.run(now, cmd)
		case c >= 0x20:
			s.line += string(c)
			if s.mode != "vim" {
				s.buf += string(c)
			}
		}
	}
}

func (s *fakeShell) recall() {
	if s.histPos == 0 {
		return
	}
	s.histPos--
	s.line = s.hist[s.histPos]
	s.buf = s.buf[:strings.LastIndex(s.buf, "\n")+1] + "$ " + s.line
}

func (s *fakeShell) queue(now time.Duration, p pending) {
	p.at += now
	s.pend = append(s.pend, p)
	sort.SliceStable(s.pend, func(i, j int) bool { return s.pend[i].at < s.pend[j].at })
}

// run schedules the output of a command.
func (s *fakeShell) run(now time.Duration, cmd string) {
	reply := func(after time.Duration, text string) {
		s.queue(now, pending{at: after, text: text, cancel: true})
	}
	switch {
	case cmd == "":
		reply(s.latency, "$ ")
	case cmd == "clear":
		s.queue(now, pending{at: s.latency, clear: true, text: "$ ", cancel: true})
	case cmd == "exit":
		s.queue(now, pending{at: s.latency, exit: new(int)})
	case strings.HasPrefix(cmd, "vim "):
		s.queue(now, pending{at: s.latency, clear: true, text: strings.Repeat("~\n", 5), mode: "vim"})
	case cmd == "git log --oneline":
		reply(s.latency, "a1b2c3d initial commit\n$ ")
	case cmd == "./bin/app --version":
		reply(s.latency, "app v1.2.3\n$ ")
	case strings.HasPrefix(cmd, "./bin/app --help"):
		reply(s.latency, "Usage: app [flags]\n  -h  help\n  -v  version\n  -q  quiet\n  -x  extra\n$ ")
	case cmd == "sleep 30":
		// Silent for 30 s; Ctrl-C ends it.
		reply(30*time.Second, "$ ")
	case strings.HasPrefix(cmd, "make build"):
		for i := 1; i <= 9; i++ {
			reply(time.Duration(i)*10*time.Second, fmt.Sprintf("compiling %d/9\n", i))
		}
		reply(95*time.Second, "BUILD-STATUS=0\n$ ")
	case cmd == "spin":
		// Output every 100 ms for a minute: never quiet for 200 ms.
		for i := 1; i <= 600; i++ {
			reply(time.Duration(i)*100*time.Millisecond, "tick\n")
		}
		reply(61*time.Second, "$ ")
	case strings.HasPrefix(cmd, "echo ") && !strings.Contains(cmd, ">"):
		reply(s.latency, strings.ReplaceAll(strings.TrimPrefix(cmd, "echo "), "'", "")+"\n$ ")
	default:
		reply(s.latency, "$ ")
	}
}

func (s *fakeShell) Shutdown(now time.Duration) {
	s.queue(now, pending{at: 10 * time.Millisecond, exit: new(int)})
}

func (s *fakeShell) Advance(now time.Duration) {
	for len(s.pend) > 0 && s.pend[0].at <= now {
		p := s.pend[0]
		s.pend = s.pend[1:]
		if p.clear {
			s.buf = ""
		}
		if p.mode != "" {
			s.mode = p.mode
		}
		s.buf += p.text
		if p.exit != nil {
			s.exit = p.exit
		}
	}
}

func (s *fakeShell) Next() (time.Duration, bool) {
	if len(s.pend) == 0 {
		return 0, false
	}
	return s.pend[0].at, true
}

func (s *fakeShell) TakeExit() (int, bool) {
	if s.exit == nil || s.taken {
		return 0, false
	}
	s.taken = true
	return *s.exit, true
}

// screenSeq is a world whose screen is a fixed sequence of texts; writes are
// ignored. Each change is applied at its time, and the child can exit at a
// time.
type screenSeq struct {
	changes []seqChange
	text    string
	now     time.Duration
	exit    *int
	exitAt  time.Duration
	taken   bool
	shut    bool
}

type seqChange struct {
	at   time.Duration
	text string
}

func (s *screenSeq) Text() string                   { return s.text }
func (s *screenSeq) ShellForeground() (bool, error) { return true, nil }
func (s *screenSeq) Write(time.Duration, []byte)    {}
func (s *screenSeq) Shutdown(time.Duration)         { s.shut = true }

func (s *screenSeq) Advance(now time.Duration) {
	s.now = now
	for len(s.changes) > 0 && s.changes[0].at <= now {
		s.text = s.changes[0].text
		s.changes = s.changes[1:]
	}
}

func (s *screenSeq) Next() (time.Duration, bool) {
	best, ok := time.Duration(0), false
	if len(s.changes) > 0 {
		best, ok = s.changes[0].at, true
	}
	if s.exit != nil && !s.taken && (!ok || s.exitAt < best) {
		best, ok = s.exitAt, true
	}
	return best, ok
}

func (s *screenSeq) TakeExit() (int, bool) {
	if s.exit == nil || s.taken || s.exitAt > s.now {
		return 0, false
	}
	s.taken = true
	return *s.exit, true
}

func readScript(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
