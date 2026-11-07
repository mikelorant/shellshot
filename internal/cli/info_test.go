package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

func infoFixtureHead() head.Head {
	return head.Head{
		Cols: 80, Rows: 24,
		StartedUnixNS: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC).UnixNano(),
		Engine:        head.Engine{Name: "ghostty", Commit: "abc1234"},
		Argv:          []string{"ls", "-l"},
		Recorder:      "shellshot 0.0.0-test",
		Flags:         head.FlagInputRecorded,
	}
}

// infoFixture is an uncompressed (so its size is stable) recording with every
// event kind in two blocks.
func infoFixture(t *testing.T) (data []byte, firstBlockEnd int) {
	data = buildRecording(t, infoFixtureHead(), bin.WriterOptions{Uncompressed: true}, func(w *bin.Writer, buf *bytes.Buffer) {
		must(t, w.Output(100*time.Millisecond, []byte("hello\r\n")))
		must(t, w.Input(200*time.Millisecond, []byte("ls\r")))
		must(t, w.Flush())
		firstBlockEnd = buf.Len()
		must(t, w.Resize(300*time.Millisecond, 100, 30, 0, 0))
		must(t, w.Mark(400*time.Millisecond, "step"))
		must(t, w.Output(1500*time.Millisecond, []byte("world")))
		must(t, w.Exit(2*time.Second, 3))
	})
	return data, firstBlockEnd
}

func runInfoCmd(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(context.Background(), append([]string{"info"}, args...), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestInfoCompleteGolden(t *testing.T) {
	data, _ := infoFixture(t)
	path := writeFile(t, "demo.shot", data)
	code, stdout, stderr := runInfoCmd(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	stdout = strings.Replace(stdout, path, "FILE", 1)
	want := `file          FILE
size          80x24
started       2025-01-02T03:04:05Z
duration      2s
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        2
events        6
  OUT         2
  IN          1
  RESIZE      1
  MARK        1
  EXIT        1
  other       0
out bytes     12
payload bytes 24
file bytes    180
file/payload  7.50
status        complete
`
	if stdout != want {
		t.Errorf("info output:\n%s\nwant:\n%s", stdout, want)
	}
}

// infoInterruptedFixture is infoFixture's recorder killed after its second
// block was flushed: the same events without the EXIT, never closed.
func infoInterruptedFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := bin.NewWriter(&buf, infoFixtureHead(), bin.WriterOptions{Uncompressed: true, Now: fixedNow})
	must(t, err)
	must(t, w.Output(100*time.Millisecond, []byte("hello\r\n")))
	must(t, w.Input(200*time.Millisecond, []byte("ls\r")))
	must(t, w.Flush())
	must(t, w.Resize(300*time.Millisecond, 100, 30, 0, 0))
	must(t, w.Mark(400*time.Millisecond, "step"))
	must(t, w.Output(1500*time.Millisecond, []byte("world")))
	must(t, w.Flush())
	return buf.Bytes()
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestInfoTruncated(t *testing.T) {
	data, firstEnd := infoFixture(t)
	path := writeFile(t, "torn.shot", data[:len(data)-2])
	code, stdout, stderr := runInfoCmd(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"blocks        1\n",
		"events        2\n",
		"status        truncated (" + itoa(firstEnd) + " bytes valid)\n",
		"input         recorded\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
}

// infoGoldenOutput runs info on data and returns stdout with the file name
// replaced by FILE.
func infoGoldenOutput(t *testing.T, data []byte, wantCode int) string {
	t.Helper()
	path := writeFile(t, "demo.shot", data)
	code, stdout, _ := runInfoCmd(t, path)
	if code != wantCode {
		t.Fatalf("exit %d, want %d", code, wantCode)
	}
	return strings.Replace(stdout, path, "FILE", 1)
}

// A recorder killed at a block boundary leaves a clean file without an EXIT
// event: the status says it was interrupted, and info still succeeds.
func TestInfoInterruptedGolden(t *testing.T) {
	got := infoGoldenOutput(t, infoInterruptedFixture(t), 0)
	want := `file          FILE
size          80x24
started       2025-01-02T03:04:05Z
duration      1.5s
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        2
events        5
  OUT         2
  IN          1
  RESIZE      1
  MARK        1
  EXIT        0
  other       0
out bytes     12
payload bytes 23
file bytes    174
file/payload  7.57
status        interrupted
`
	if got != want {
		t.Errorf("info output:\n%s\nwant:\n%s", got, want)
	}
}

// A file cut inside its second chunk keeps the first chunk and reports how
// many bytes of it are valid.
func TestInfoTruncatedGolden(t *testing.T) {
	data, firstEnd := infoFixture(t)
	if firstEnd != 130 {
		t.Fatalf("fixture first block ends at %d, the golden expects 130", firstEnd)
	}
	got := infoGoldenOutput(t, data[:len(data)-2], 0)
	want := `file          FILE
size          80x24
started       2025-01-02T03:04:05Z
duration      200ms
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        1
events        2
  OUT         1
  IN          1
  RESIZE      0
  MARK        0
  EXIT        0
  other       0
out bytes     7
payload bytes 10
file bytes    178
file/payload  17.80
status        truncated (130 bytes valid)
`
	if got != want {
		t.Errorf("info output:\n%s\nwant:\n%s", got, want)
	}
}

// A corrupt chunk with data after it is damage: the report shows what was
// read and the command fails.
func TestInfoDamagedGolden(t *testing.T) {
	data, firstEnd := infoFixture(t)
	bad := append([]byte(nil), data...)
	bad[firstEnd-10] ^= 0xff
	got := infoGoldenOutput(t, bad, 1)
	want := `file          FILE
size          80x24
started       2025-01-02T03:04:05Z
duration      0s
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        0
events        0
  OUT         0
  IN          0
  RESIZE      0
  MARK        0
  EXIT        0
  other       0
out bytes     0
payload bytes 0
file bytes    180
file/payload  -
status        damaged: bin: corrupt recording: chunk: crc mismatch: chunk EVNT at offset 85
`
	if got != want {
		t.Errorf("info output:\n%s\nwant:\n%s", got, want)
	}
}

// A keystroke can be logged after the child's exit; the recording is still
// complete.
func TestInfoCompleteWhenExitIsNotLast(t *testing.T) {
	data := buildRecording(t, infoFixtureHead(), bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(100*time.Millisecond, []byte("bye")))
		must(t, w.Exit(200*time.Millisecond, 0))
		must(t, w.Input(210*time.Millisecond, []byte("x")))
	})
	code, stdout, stderr := runInfoCmd(t, writeFile(t, "late.shot", data))
	if code != 0 || stderr != "" || !strings.Contains(stdout, "status        complete\n") {
		t.Errorf("exit %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}
}

// The three non-error statuses are explained in --help.
func TestInfoHelpExplainsStatuses(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := Run(context.Background(), []string{"info", "--help"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	for _, want := range []string{"complete ", "interrupted ", "truncated (N bytes valid)", "damaged: ..."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestInfoDefaultsForMinimalHeader(t *testing.T) {
	data := buildRecording(t, head.Head{Cols: 5, Rows: 2}, bin.WriterOptions{}, func(*bin.Writer, *bytes.Buffer) {})
	path := writeFile(t, "min.shot", data)
	code, stdout, stderr := runInfoCmd(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"started       -\n", "duration      0s\n", "recorder      -\n", "engine        -\n", "command       -\n",
		"input         not recorded\n", "blocks        0\n", "file/payload  -\n", "status        interrupted\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
}

func TestInfoErrors(t *testing.T) {
	data, firstEnd := infoFixture(t)
	bad := append([]byte(nil), data...)
	bad[firstEnd-10] ^= 0xff
	tests := []struct {
		name string
		path string
		want string
	}{
		{"missing", filepath.Join(t.TempDir(), "nope.shot"), "no such file"},
		{"json", writeFile(t, "old.jsonl", []byte(`{"version":1}`+"\n")), "not a shellshot recording (bad magic)"},
		{"corrupt", writeFile(t, "bad.shot", bad), "corrupt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runInfoCmd(t, tt.path)
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q, want exit 1 containing %q", code, stderr, tt.want)
			}
		})
	}
	if code, _, stderr := runInfoCmd(t); code != 1 || !strings.Contains(stderr, "accepts 1 arg") {
		t.Errorf("no argument: exit %d, stderr %q", code, stderr)
	}
}

// spanFixture has two scenes, a hidden span inside the first and one at top
// level, and a third scene that is never closed.
func spanFixture(t *testing.T, exit bool) []byte {
	return buildRecording(t, infoFixtureHead(), bin.WriterOptions{Uncompressed: true}, func(w *bin.Writer, _ *bytes.Buffer) {
		scene := func(at time.Duration, id uint64, name string, h event.SpanHints) {
			must(t, w.Span(at, event.Span{Op: event.SpanBegin, ID: id, Type: event.SpanScene, Name: name, Hints: h}))
		}
		hidden := func(at time.Duration, id, parent uint64) {
			must(t, w.Span(at, event.Span{Op: event.SpanBegin, ID: id, Type: event.SpanHidden, Parent: parent}))
		}
		end := func(at time.Duration, id uint64) { must(t, w.Span(at, event.Span{Op: event.SpanEnd, ID: id})) }

		hidden(0, 1, 0)
		must(t, w.Output(500*time.Millisecond, []byte("setup")))
		end(time.Second, 1)
		scene(2300*time.Millisecond, 2, "install", event.SpanHints{Speed: 8000, Idle: 2 * time.Second, Hold: time.Second})
		hidden(5*time.Second, 3, 2)
		end(7*time.Second, 3)
		end(17900*time.Millisecond, 2)
		scene(65*time.Second, 4, "outro", event.SpanHints{Speed: 1500})
		must(t, w.Output(70*time.Second, []byte("bye")))
		if exit {
			must(t, w.Exit(71*time.Second, 0))
		}
	})
}

func TestInfoListsSpansGolden(t *testing.T) {
	path := writeFile(t, "spans.shot", spanFixture(t, false))
	code, stdout, stderr := runInfoCmd(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	stdout = strings.Replace(stdout, path, "FILE", 1)
	want := `file          FILE
size          80x24
started       2025-01-02T03:04:05Z
duration      1m10s
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        1
events        9
  OUT         2
  IN          0
  RESIZE      0
  MARK        0
  EXIT        0
  other       7
scenes        2
  2 install  0:02.3 +15.6s  speed 8x idle 2s hold 1s
  4 outro  1:05.0 +5.0s  speed 1.5x  (open)
hidden        2 spans, 3.0s
out bytes     8
payload bytes 75
file bytes    228
file/payload  3.04
status        interrupted
`
	if stdout != want {
		t.Errorf("info output:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestInfoWithoutSpansHasNoSpanLines(t *testing.T) {
	data, _ := infoFixture(t)
	_, stdout, _ := runInfoCmd(t, writeFile(t, "demo.shot", data))
	if strings.Contains(stdout, "scenes") || strings.Contains(stdout, "hidden") {
		t.Errorf("unexpected span lines:\n%s", stdout)
	}
}
