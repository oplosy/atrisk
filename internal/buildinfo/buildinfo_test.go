package buildinfo

import "testing"

func TestFormat(t *testing.T) {
	if got := Format("component", "1.2.3"); got != "atlasrisk component version 1.2.3" {
		t.Fatalf("unexpected diagnostic: %q", got)
	}
}
