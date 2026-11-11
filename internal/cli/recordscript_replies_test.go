package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikelorant/shellshot/internal/render/theme"
)

// writeHelper writes a POSIX sh program to a temporary file and returns its
// path, for a script to run.
func writeHelper(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// queryHelper asks the terminal for its background colour (OSC 11) and the
// cursor position (DSR 6), waits for both answers in raw mode, and prints what
// it received. Without an answering terminal it blocks in dd until the script
// gives up.
const queryHelper = `stty raw -echo
printf '\033]11;?\033\\\033[6n'
got=
while :; do
  c=$(dd bs=1 count=1 2>/dev/null)
  case $c in R) break ;; esac
  got="$got$c"
done
stty sane
case $got in
  *"]11;rgb:%[1]s"*) echo "GOT-BACKGROUND" ;;
  *) echo "BAD-BACKGROUND" ;;
esac
case $got in
  *"["[0-9]*";"[0-9]*) echo "GOT-CURSOR" ;;
  *) echo "BAD-CURSOR" ;;
esac
`

func TestRecordScriptAnswersTerminalQueries(t *testing.T) {
	t.Parallel()
	bg := theme.Default().Background
	want := fmt.Sprintf("%02x%02x/%02x%02x/%02x%02x", bg.R, bg.R, bg.G, bg.G, bg.B, bg.B)
	helper := writeHelper(t, fmt.Sprintf(queryHelper, want))

	r := runScriptCLI(t, "run sh "+helper+"\nwait /^GOT-BACKGROUND$/ 10s\nwait /^GOT-CURSOR$/ 0s\n")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0 (the program should have received the replies)\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
}

// floodHelper asks for the cursor position thousands of times, printing a
// line of output after each, and never reads its input. The replies fill the
// pty input queue, so a terminal that wrote them in the loop that reads the
// program's output would stop reading while the program waits to write.
//
// It turns echo off first: with echo on, the line discipline of the pty (on
// Linux) writes every unread reply back into the output as text such as
// "^[[8;1R", without a newline, so the final line can be "^[[8;1RFLOOD-DONE"
// and never match the wait.
const floodHelper = `stty -echo
i=0
while [ $i -lt 3000 ]; do
  printf '\033[6n%0100d\n' 0
  i=$((i+1))
done
echo FLOOD-DONE
`

func TestRecordScriptQueryFloodDoesNotStall(t *testing.T) {
	t.Parallel()
	helper := writeHelper(t, floodHelper)

	r := runScriptCLI(t, "settle off\nrun sh "+helper+"\nwait /^FLOOD-DONE$/ 20s\n")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
}
