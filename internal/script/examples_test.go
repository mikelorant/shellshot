package script_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/michaellorant/shellshot/internal/script"
)

// TestExampleScripts parses and validates every examples/scripts/*.script file.
func TestExampleScripts(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot find the test file path")
	}
	pattern := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "scripts", "*.script")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no example scripts found with %s", pattern)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			_, diags := script.Check(file, src)
			for _, d := range diags {
				t.Errorf("%s:%s", filepath.Base(file), d.Error())
			}
		})
	}
}
