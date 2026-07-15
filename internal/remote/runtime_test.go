package remote

import "testing"

func TestMapRuntime(t *testing.T) {
	cases := []struct {
		unameS, unameM string
		want           string
	}{
		{"Linux", "x86_64", "linux-amd64"},
		{"Linux", "aarch64", "linux-arm64"},
		{"Darwin", "arm64", "darwin-arm64"},
	}
	for _, tc := range cases {
		got, err := mapRuntime(tc.unameS, tc.unameM)
		if err != nil {
			t.Fatalf("mapRuntime(%q,%q) error: %v", tc.unameS, tc.unameM, err)
		}
		if got != tc.want {
			t.Fatalf("mapRuntime(%q,%q) = %q, want %q", tc.unameS, tc.unameM, got, tc.want)
		}
	}
}

func TestMapRuntime_Unsupported(t *testing.T) {
	if _, err := mapRuntime("Windows", "x86_64"); err == nil {
		t.Fatal("expected error for unsupported runtime")
	}
}
