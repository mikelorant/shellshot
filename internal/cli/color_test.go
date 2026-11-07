package cli

import "testing"

func TestResolveColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mode    string
		isTTY   bool
		noColor string
		want    bool
		wantErr bool
	}{
		{"auto tty", "auto", true, "", true, false},
		{"auto pipe", "auto", false, "", false, false},
		{"auto tty NO_COLOR set", "auto", true, "1", false, false},
		{"auto pipe NO_COLOR set", "auto", false, "1", false, false},
		{"always pipe", "always", false, "", true, false},
		{"always ignores NO_COLOR", "always", false, "1", true, false},
		{"never tty", "never", true, "", false, false},
		{"empty mode", "", true, "", false, true},
		{"unknown mode", "sometimes", true, "", false, true},
		{"wrong case", "Always", true, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveColor(tt.mode, tt.isTTY, tt.noColor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveColor(%q, %v, %q) error = %v, wantErr %v", tt.mode, tt.isTTY, tt.noColor, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveColor(%q, %v, %q) = %v, want %v", tt.mode, tt.isTTY, tt.noColor, got, tt.want)
			}
		})
	}
}
