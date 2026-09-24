package main

import (
	"testing"

	"github.com/oplosy/atrisk/internal/buildinfo"
)

func TestVersionFormat(t *testing.T) {
	got := buildinfo.Format("collector", "test")
	if got != "atlasrisk collector version test" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
