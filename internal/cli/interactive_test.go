package cli

import (
	"bytes"
	"reflect"
	"testing"
)

func TestResolveCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		argv    []string
		script  string
		want    []string
		wantErr bool
	}{
		{name: "default shell", want: []string{"/bin/zsh"}},
		{name: "command string", script: "echo hi", want: []string{"/bin/zsh", "-c", "echo hi"}},
		{name: "argv", argv: []string{"ls", "-l"}, want: []string{"ls", "-l"}},
		{name: "conflict", argv: []string{"ls"}, script: "echo hi", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCommand(tt.argv, tt.script, "/bin/zsh")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("command = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShellFromEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"SHELL": "/bin/fish"}
	if got := shellFromEnv(func(k string) string { return env[k] }); got != "/bin/fish" {
		t.Errorf("shell = %q, want /bin/fish", got)
	}
	if got := shellFromEnv(func(string) string { return "" }); got != "/bin/sh" {
		t.Errorf("shell = %q, want /bin/sh fallback", got)
	}
}

func TestResolveSize(t *testing.T) {
	t.Parallel()
	var notTerminal bytes.Buffer
	tests := []struct {
		name                     string
		cols, rows, wantC, wantR int
	}{
		{"both auto falls back to 80x24", 0, 0, 80, 24},
		{"explicit", 40, 6, 40, 6},
		{"cols only", 100, 0, 100, 24},
		{"rows only", 0, 10, 80, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r := resolveSize(tt.cols, tt.rows, &notTerminal)
			if c != tt.wantC || r != tt.wantR {
				t.Errorf("size = %dx%d, want %dx%d", c, r, tt.wantC, tt.wantR)
			}
		})
	}
}
