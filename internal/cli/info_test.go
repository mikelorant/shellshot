package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

// infoFixture is an uncompressed (so its size is stable) recording with every
// event kind in two blocks.
func infoFixture(t *testing.T) (data []byte, firstBlockEnd int) {
	h := head.Head{
		Cols: 80, Rows: 24,
		StartedUnixNS: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC).UnixNano(),
		Engine:        head.Engine{Name: "ghostty", Commit: "abc1234"},
		Argv:          []string{"ls", "-l"},
		Recorder:      "shellshot 0.0.0-test",
		Flags:         head.FlagInputRecorded,
	}
	data = buildRecording(t, h, bin.WriterOptions{Uncompressed: true}, func(w *bin.Writer, buf *bytes.Buffer) {
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

func TestInfoGolden(t *testing.T) {
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

func TestInfoDefaultsForMinimalHeader(t *testing.T) {
	data := buildRecording(t, head.Head{Cols: 5, Rows: 2}, bin.WriterOptions{}, func(*bin.Writer, *bytes.Buffer) {})
	path := writeFile(t, "min.shot", data)
	code, stdout, stderr := runInfoCmd(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"started       -\n", "duration      0s\n", "recorder      -\n", "engine        -\n", "command       -\n",
		"input         not recorded\n", "blocks        0\n", "file/payload  -\n", "status        complete\n",
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
