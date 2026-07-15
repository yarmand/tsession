package cmd

import (
	"strings"
	"testing"
)

func TestRemoteServeCommand_Dispatches(t *testing.T) {
	err := Remote([]string{"serve"})
	if err != nil && !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemote_UnknownSubcommand(t *testing.T) {
	err := Remote([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown remote subcommand") {
		t.Fatalf("err = %v, want unknown remote subcommand error", err)
	}
}

func TestRemote_NoSubcommand(t *testing.T) {
	err := Remote([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("err = %v, want usage error", err)
	}
}
