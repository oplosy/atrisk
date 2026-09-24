package main

import (
	"testing"

	"github.com/oplosy/atrisk/internal/buildinfo"
)

func TestVersionFormat(t *testing.T) {
	got := buildinfo.Format("api", "test")
	if got != "atlasrisk api version test" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
