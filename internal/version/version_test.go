package version

import "testing"

func TestCurrentTag(t *testing.T) {
	orig := tag
	t.Cleanup(func() { tag = orig })

	tag = "v1.2.3"
	if got := CurrentTag(); got != "v1.2.3" {
		t.Fatalf("CurrentTag() = %q, want v1.2.3", got)
	}
}

func TestCurrentTag_EmptyWhenUnset(t *testing.T) {
	orig := tag
	t.Cleanup(func() { tag = orig })

	tag = ""
	if got := CurrentTag(); got != "" {
		t.Fatalf("CurrentTag() = %q, want empty", got)
	}
}
