package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/codec"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestBuildHead(t *testing.T) {
	t.Parallel()
	started := time.Date(2024, 1, 2, 3, 4, 5, 6, time.FixedZone("x", 3600))
	id := [16]byte{1, 2, 3}
	h := buildHead(headInputs{
		Cols: 80, Rows: 24,
		Argv:    []string{"/bin/sh", "-c", "ls"},
		Started: started,
		Getenv: envFrom(map[string]string{
			"TERM": "screen", "COLORTERM": "truecolor", "LANG": "C", "SHELL": "/bin/zsh",
			"HOME": "/home/me", "SECRET_TOKEN": "x", "PWD": "/home/me/work",
		}),
		SessionID: id,
	})

	if h.Cols != 80 || h.Rows != 24 {
		t.Errorf("size = %dx%d", h.Cols, h.Rows)
	}
	if !reflect.DeepEqual(h.Argv, []string{"/bin/sh", "-c", "ls"}) {
		t.Errorf("argv = %q", h.Argv)
	}
	wantEnv := []string{"TERM=screen", "COLORTERM=truecolor", "LANG=C", "SHELL=/bin/zsh"}
	if !reflect.DeepEqual(h.Env, wantEnv) {
		t.Errorf("env = %q, want only the allowlist %q", h.Env, wantEnv)
	}
	if h.Term != "screen" {
		t.Errorf("term = %q, want screen", h.Term)
	}
	if h.StartedUnixNS != started.UnixNano() || h.TZOffsetS != 3600 {
		t.Errorf("start = %d tz %d", h.StartedUnixNS, h.TZOffsetS)
	}
	if h.Engine != (head.Engine{Name: "libghostty-vt", Commit: "66af4c7458db7e19ae7adb44aa7054879be58b6b"}) {
		t.Errorf("engine = %+v", h.Engine)
	}
	if h.SessionID != id || h.Recorder != "shellshot "+Version {
		t.Errorf("session id / recorder = %x / %q", h.SessionID, h.Recorder)
	}
	if h.Cwd != "" || h.Hostname != "" || h.Title != "" {
		t.Errorf("cwd %q hostname %q title %q recorded, want none", h.Cwd, h.Hostname, h.Title)
	}
	if h.Flags != 0 || h.TickNS != 0 {
		t.Errorf("flags %#x tick %d, want defaults", h.Flags, h.TickNS)
	}
}

func TestBuildHeadTermFallbackMatchesChildEnvironment(t *testing.T) {
	t.Parallel()
	h := buildHead(headInputs{Cols: 1, Rows: 1, Getenv: envFrom(nil)})
	if h.Term != "xterm-256color" || !reflect.DeepEqual(h.Env, []string{"TERM=xterm-256color"}) {
		t.Errorf("term %q env %q, want the TERM the child gets", h.Term, h.Env)
	}
}

func TestBuildHeadRecordInputFlag(t *testing.T) {
	t.Parallel()
	h := buildHead(headInputs{Cols: 1, Rows: 1, Getenv: envFrom(nil), RecordInput: true})
	if h.Flags != head.FlagInputRecorded {
		t.Errorf("flags = %#x, want input-recorded only", h.Flags)
	}
}

func TestNewSessionIDIsRandom(t *testing.T) {
	t.Parallel()
	a, err := newSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newSessionID()
	if a == ([16]byte{}) || a == b {
		t.Errorf("session ids %x and %x, want distinct non-zero", a, b)
	}
}

func TestWriterOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		codec     string
		coalesce  time.Duration
		want      bin.WriterOptions
		wantError bool
	}{
		{name: "zstd", codec: "zstd", want: bin.WriterOptions{Codec: codec.Zstd}},
		{name: "flate", codec: "flate", want: bin.WriterOptions{Codec: codec.Flate}},
		{name: "none", codec: "none", want: bin.WriterOptions{Uncompressed: true}},
		{name: "coalesce ticks", codec: "zstd", coalesce: 5 * time.Millisecond, want: bin.WriterOptions{Codec: codec.Zstd, CoalesceTicks: 50}},
		{name: "sub-tick coalesce rounds up", codec: "zstd", coalesce: time.Nanosecond, want: bin.WriterOptions{Codec: codec.Zstd, CoalesceTicks: 1}},
		{name: "unknown codec", codec: "lz4", wantError: true},
		{name: "negative coalesce", codec: "zstd", coalesce: -time.Second, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := writerOptions(tt.codec, tt.coalesce)
			if (err != nil) != tt.wantError {
				t.Fatalf("err = %v, wantError %v", err, tt.wantError)
			}
			if !tt.wantError && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("options = %+v, want %+v", got, tt.want)
			}
		})
	}
}
