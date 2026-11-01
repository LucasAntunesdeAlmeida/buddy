package main

import (
	"flag"
	"io"
	"path/filepath"
	"slices"
	"testing"
)

func TestStartArgs(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("buddy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("voice", false, "")
	fs.Bool("v", false, "")
	fs.String("data", "", "")
	if err := fs.Parse([]string{"-voice", "-data", "brains"}); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs("brains")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-data=" + abs, "-voice=true"}
	if got := startArgs(fs); !slices.Equal(got, want) {
		t.Errorf("startArgs = %q, want %q", got, want)
	}
}
