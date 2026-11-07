package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/chunk"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

func TestRecordFlagDefaults(t *testing.T) {
	t.Parallel()
	cmd := newRecordCmd()
	for name, want := range map[string]string{
		"output":       "",
		"command":      "",
		"cols":         "0",
		"rows":         "0",
		"record-input": "false",
		"coalesce":     "0s",
		"codec":        "zstd",
	} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("missing --%s", name)
			continue
		}
		if f.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, want)
		}
	}
	if cmd.Flags().Lookup("fps") != nil {
		t.Error("record still has --fps; it no longer samples frames")
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("missing -o shorthand")
	}
	if cmd.Flags().ShorthandLookup("c") == nil {
		t.Error("missing -c shorthand")
	}
}

func TestRecordFlagParsing(t *testing.T) {
	t.Parallel()
	var ro recordOptions
	cmd := newRecordCmd()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.SetArgs([]string{"--record-input", "--coalesce", "5ms", "--codec", "flate", "-o", "x.shot"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	f := cmd.Flags()
	ro.RecordInput, _ = f.GetBool("record-input")
	ro.Coalesce, _ = f.GetDuration("coalesce")
	ro.Codec, _ = f.GetString("codec")
	if !ro.RecordInput || ro.Coalesce != 5*time.Millisecond || ro.Codec != "flate" {
		t.Errorf("flags = %+v", ro)
	}
}

func TestRecordRejectsBadFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"codec", []string{"--codec", "lz4"}, "invalid --codec"},
		{"negative coalesce", []string{"--coalesce", "-1s"}, "invalid --coalesce"},
		{"fps", []string{"--fps", "10"}, "unknown flag: --fps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "x.shot")
			var stdout, stderr bytes.Buffer
			args := append([]string{"record", "-o", path}, tt.args...)
			args = append(args, "-c", "true")
			if code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.want)
			}
			if _, err := os.Stat(path); err == nil {
				t.Error("recording file created for rejected flags")
			}
		})
	}
}

func TestRecordRejectsCommandStringWithArgv(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", filepath.Join(t.TempDir(), "x.shot"), "-c", "true", "--", "ls"}
	code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr = %q, want mutually exclusive error", stderr.String())
	}
}

// recorded is a recording read back from disk.
type recorded struct {
	head   head.Head
	theme  *head.Theme
	events []bin.Event
	// err is the error that ended the read: io.EOF or bin.ErrTruncated.
	err error
}

// out returns the concatenated payload of the OUT events.
func (r recorded) out() string { return r.concat(event.KindOut) }

// in returns the concatenated payload of the IN events.
func (r recorded) in() string { return r.concat(event.KindIn) }

func (r recorded) concat(kind event.Kind) string {
	var b strings.Builder
	for _, e := range r.events {
		if e.Kind == kind {
			b.Write(e.Data)
		}
	}
	return b.String()
}

// count returns the number of events of kind.
func (r recorded) count(kind event.Kind) int {
	n := 0
	for _, e := range r.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// readRecording reads all of data with the binary reader.
func readRecording(t *testing.T, data []byte) recorded {
	t.Helper()
	r, err := bin.NewReader(bytes.NewReader(data), bin.ReaderOptions{CopyData: true})
	if err != nil {
		t.Fatalf("bin.NewReader: %v", err)
	}
	rec := recorded{head: r.Head(), theme: r.Theme()}
	for {
		e, err := r.Next()
		if err != nil {
			rec.err = err
			return rec
		}
		rec.events = append(rec.events, e)
	}
}

// recordToFile runs args with stdin and returns the recording file's bytes
// and stderr. The command must exit 0.
func recordToFile(t *testing.T, args []string, stdin string) (data []byte, stderr string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.shot")
	code, _, stderr := runRecordWithTimeout(t, append([]string{"record", "-o", path}, args...), stdin)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	return data, stderr
}

func TestRecordWritesBinaryRecording(t *testing.T) {
	t.Parallel()
	data, stderr := recordToFile(t, []string{"--cols", "40", "--rows", "6", "-c", "printf hi; sleep 0.2; printf there"}, "")

	if !bytes.HasPrefix(data, []byte(chunk.Magic)) {
		t.Fatalf("recording starts with %q, want the binary magic", data[:8])
	}
	rec := readRecording(t, data)
	if !errors.Is(rec.err, io.EOF) {
		t.Fatalf("read ended with %v, want io.EOF", rec.err)
	}
	h := rec.head
	if h.Cols != 40 || h.Rows != 6 {
		t.Errorf("size = %dx%d, want 40x6", h.Cols, h.Rows)
	}
	if len(h.Argv) != 3 || h.Argv[1] != "-c" || h.Argv[2] != "printf hi; sleep 0.2; printf there" {
		t.Errorf("argv = %q", h.Argv)
	}
	if h.Engine.Name != "libghostty-vt" || h.Engine.Commit != ghosttyCommit {
		t.Errorf("engine = %+v", h.Engine)
	}
	if h.Flags != 0 {
		t.Errorf("flags = %#x, want none", h.Flags)
	}
	if h.StartedUnixNS == 0 || h.Recorder != "shellshot "+Version || h.SessionID == ([16]byte{}) {
		t.Errorf("head = %+v, want start time, recorder and session id", h)
	}
	if h.Cwd != "" || h.Hostname != "" {
		t.Errorf("cwd %q / hostname %q recorded, want neither", h.Cwd, h.Hostname)
	}
	if rec.theme != nil {
		t.Error("recording has a theme chunk; themes are applied at export time")
	}

	if got := rec.out(); got != "hithere" {
		t.Errorf("OUT = %q, want %q", got, "hithere")
	}
	if n := rec.count(event.KindIn); n != 0 {
		t.Errorf("%d IN events without --record-input", n)
	}
	var prev time.Duration
	var hiAt, thereAt time.Duration
	for _, e := range rec.events {
		if e.At < prev {
			t.Errorf("timestamp %v after %v: not monotonic", e.At, prev)
		}
		prev = e.At
		if e.Kind == event.KindOut {
			if strings.Contains(string(e.Data), "hi") && hiAt == 0 {
				hiAt = e.At
			}
			if strings.Contains(string(e.Data), "there") {
				thereAt = e.At
			}
		}
	}
	if gap := thereAt - hiAt; gap < 150*time.Millisecond {
		t.Errorf("gap between outputs = %v, want >= 150ms", gap)
	}
	last := rec.events[len(rec.events)-1]
	if last.Kind != event.KindExit {
		t.Fatalf("last event kind = %v, want exit", last.Kind)
	}
	if status, err := event.ParseExit(last.Data); err != nil || status != 0 {
		t.Errorf("exit = (%d, %v), want (0, nil)", status, err)
	}

	if !strings.Contains(stderr, " events (") || !strings.Contains(stderr, " KB) to ") {
		t.Errorf("stderr = %q, want recorded summary", stderr)
	}
	if strings.Contains(stderr, "keyboard input") {
		t.Errorf("stderr = %q, want no input notice", stderr)
	}
}

func TestRecordExitStatusIsInRecording(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "-c", "exit 7"}
	if code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 7 {
		t.Fatalf("exit code = %d, want 7 (stderr %q)", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := readRecording(t, data)
	last := rec.events[len(rec.events)-1]
	if status, err := event.ParseExit(last.Data); last.Kind != event.KindExit || err != nil || status != 7 {
		t.Errorf("last event = %+v (status %d, %v), want exit 7", last, status, err)
	}
}

func TestRecordRecordInput(t *testing.T) {
	t.Parallel()
	data, stderr := recordToFile(t, []string{"--cols", "40", "--rows", "6", "--record-input", "-c", "read x; echo got:$x"}, "hello\n")

	rec := readRecording(t, data)
	if rec.head.Flags&head.FlagInputRecorded == 0 {
		t.Errorf("flags = %#x, want input-recorded bit", rec.head.Flags)
	}
	if got := rec.in(); !strings.HasPrefix(got, "hello\n") {
		t.Errorf("IN = %q, want it to start with hello\\n", got)
	}
	if !strings.Contains(rec.out(), "got:hello") {
		t.Errorf("OUT = %q, want it to contain got:hello", rec.out())
	}
	if !strings.Contains(stderr, inputNotice) {
		t.Errorf("stderr = %q, want the keyboard input notice", stderr)
	}
	var prev time.Duration
	for _, e := range rec.events {
		if e.At < prev {
			t.Errorf("timestamp %v after %v: not monotonic across IN and OUT", e.At, prev)
		}
		prev = e.At
	}
}

func TestRecordInputSharesOriginWithOutput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.shot")
	pr, pw := io.Pipe()
	defer pw.Close()
	// The input is sent 300 ms after the command has printed "ready", so the
	// session has started by then, however long starting it took under load.
	ready := &signalWriter{want: "ready", seen: make(chan struct{})}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ready.seen:
		case <-stop:
			return
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = pw.Write([]byte("late\n"))
	}()
	var stderr bytes.Buffer
	stdout := ready
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "6", "--record-input", "-c", "echo ready; read x; echo got:$x"}
	done := make(chan int, 1)
	go func() { done <- runWithStdin(context.Background(), args, pr, stdout, &stderr) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d (stderr %q)", code, stderr.String())
		}
	case <-time.After(recordTimeout):
		t.Fatal("record did not finish")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := readRecording(t, data)
	var inAt, gotAt time.Duration = -1, -1
	for _, e := range rec.events {
		switch {
		case e.Kind == event.KindIn && inAt < 0:
			inAt = e.At
		case e.Kind == event.KindOut && strings.Contains(string(e.Data), "got:late"):
			gotAt = e.At
		}
	}
	if inAt < 250*time.Millisecond {
		t.Errorf("IN at %v, want >= 250ms: input must share the session's time origin", inAt)
	}
	if gotAt < inAt || gotAt-inAt > time.Second {
		t.Errorf("OUT got:late at %v, IN at %v: want output shortly after the input", gotAt, inAt)
	}
}

// signalWriter closes seen once what was written to it contains want.
type signalWriter struct {
	want string
	seen chan struct{}

	mu   sync.Mutex
	buf  bytes.Buffer
	done bool
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if !w.done && strings.Contains(w.buf.String(), w.want) {
		w.done = true
		close(w.seen)
	}
	return len(p), nil
}

func TestRecordCoalesceMergesOutputs(t *testing.T) {
	t.Parallel()
	script := "printf a; sleep 0.02; printf b; sleep 0.02; printf c"
	plain, _ := recordToFile(t, []string{"--cols", "40", "--rows", "6", "-c", script}, "")
	merged, _ := recordToFile(t, []string{"--cols", "40", "--rows", "6", "--coalesce", "1s", "-c", script}, "")

	p, m := readRecording(t, plain), readRecording(t, merged)
	if p.out() != "abc" || m.out() != "abc" {
		t.Fatalf("OUT = %q and %q, want abc", p.out(), m.out())
	}
	if p.head.Flags&head.FlagCoalesced != 0 {
		t.Errorf("flags = %#x without --coalesce, want no coalesced bit", p.head.Flags)
	}
	if m.head.Flags&head.FlagCoalesced == 0 {
		t.Errorf("flags = %#x with --coalesce, want the coalesced bit", m.head.Flags)
	}
	if n := m.count(event.KindOut); n != 1 {
		t.Errorf("coalesced OUT events = %d, want 1", n)
	}
	if n := p.count(event.KindOut); n < 2 {
		t.Errorf("uncoalesced OUT events = %d, want several", n)
	}
}

func TestRecordCodecs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"none", "flate", "zstd"} {
		t.Run(name, func(t *testing.T) {
			data, _ := recordToFile(t, []string{"--cols", "40", "--rows", "6", "--codec", name, "-c", "seq 1 200"}, "")
			rec := readRecording(t, data)
			if !errors.Is(rec.err, io.EOF) || !strings.Contains(rec.out(), "200") {
				t.Errorf("read = (%v, %q), want io.EOF and the output", rec.err, rec.out())
			}
		})
	}
}

func TestRecordTruncatedFileReadsUpToLastGoodBlock(t *testing.T) {
	t.Parallel()
	data, _ := recordToFile(t, []string{"--cols", "40", "--rows", "6", "-c", "printf hi"}, "")
	full := readRecording(t, data)
	cut := readRecording(t, data[:len(data)-10])
	if !errors.Is(cut.err, bin.ErrTruncated) {
		t.Fatalf("read of truncated file ended with %v, want bin.ErrTruncated", cut.err)
	}
	if len(cut.events) >= len(full.events) {
		t.Errorf("truncated file has %d events, full has %d", len(cut.events), len(full.events))
	}
	if cut.head.Cols != 40 {
		t.Errorf("truncated head = %+v", cut.head)
	}
}

func TestRecordForwardsStdinAndShowsOutputLive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "6", "-c", "read x; echo got:$x"}
	code := runWithStdin(context.Background(), args, strings.NewReader("hi\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "got:hi") {
		t.Errorf("live stdout = %q, want it to contain got:hi", stdout.String())
	}
	if strings.Contains(stderr.String(), "recording —") {
		t.Errorf("stderr = %q, want no interactive hint for non-terminal stdin", stderr.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	rec := readRecording(t, data)
	if h := rec.head; h.Cols != 40 || h.Rows != 6 || len(h.Argv) != 3 || h.Argv[1] != "-c" {
		t.Errorf("head = %+v", h)
	}
	if !strings.Contains(rec.out(), "got:hi") {
		t.Errorf("OUT = %q, want it to contain got:hi", rec.out())
	}
}

// recordTimeout bounds record runs fed from a finite stdin, so a regression
// that hangs fails fast.
const recordTimeout = 10 * time.Second

// runRecordWithTimeout runs args with stdin and fails the test if Run does not
// return within recordTimeout. An empty stdin is a pipe that stays open, not
// an immediate EOF.
func runRecordWithTimeout(t *testing.T, args []string, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var in io.Reader = strings.NewReader(stdin)
	if stdin == "" {
		pr, pw := io.Pipe()
		t.Cleanup(func() { pw.Close() })
		in = pr
	}
	var out, errOut bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runWithStdin(ctx, args, in, &out, &errOut) }()
	select {
	case code = <-done:
		return code, out.String(), errOut.String()
	case <-time.After(recordTimeout):
		cancel()
		t.Fatalf("record did not finish within %v after stdin EOF", recordTimeout)
		return 0, "", ""
	}
}

func TestRecordPipedStdinEOFExitsDefaultShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	data, _ := recordToFile(t, []string{"--cols", "40", "--rows", "5"}, "echo hi\n")
	if out := readRecording(t, data).out(); !strings.Contains(out, "hi") {
		t.Errorf("OUT = %q, want it to contain hi", out)
	}
}

func TestRecordPipedStdinEOFEndsCat(t *testing.T) {
	t.Parallel()
	data, _ := recordToFile(t, []string{"--cols", "40", "--rows", "5", "-c", "cat"}, "hi\n")
	if out := readRecording(t, data).out(); !strings.Contains(out, "hi") {
		t.Errorf("OUT = %q, want it to contain hi", out)
	}
}

// assertOutputContains fails unless the OUT events of the recording at path
// contain want.
func assertOutputContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	if out := readRecording(t, data).out(); !strings.Contains(out, want) {
		t.Errorf("OUT = %q, want it to contain %q", out, want)
	}
}

// TestRecordPipedStdinEOFDoesNotEchoEOT checks that the Ctrl-D forwarded at
// the end of stdin ends cat without the terminal echoing it into the
// recording.
func TestRecordPipedStdinEOFDoesNotEchoEOT(t *testing.T) {
	t.Parallel()
	data, _ := recordToFile(t, []string{"--cols", "40", "--rows", "5", "-c", "cat"}, "x\n")
	if out, want := readRecording(t, data).out(), "x\r\nx\r\n"; out != want {
		t.Errorf("OUT = %q, want %q (typed echo and cat's output, no echoed EOT)", out, want)
	}
}

// TestRecordEmptyStdinSendsNoEOT checks that an stdin already at EOF (such as
// /dev/null) records only the command's own output.
func TestRecordEmptyStdinSendsNoEOT(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "-c", "printf hi"}
	if code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	if out := readRecording(t, data).out(); out != "hi" {
		t.Errorf("OUT = %q, want exactly %q", out, "hi")
	}
}
