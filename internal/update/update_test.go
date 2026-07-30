package update

import "testing"

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"v0.2.1", "0.2.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.9", "v0.2.0", false},
		{"dev", "v0.2.0", false},
		{"v0.2.1-rc.1", "v0.2.0", true},
	} {
		if got := newer(tt.candidate, tt.current); got != tt.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tt.candidate, tt.current, got, tt.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	const sum = "0123456789012345678901234567890123456789012345678901234567890123"
	got, err := checksumFor(sum+"  rook_v0.2.1_darwin_arm64.tar.gz\n", "rook_v0.2.1_darwin_arm64.tar.gz")
	if err != nil || got != sum {
		t.Fatalf("checksumFor() = %q, %v", got, err)
	}
}
