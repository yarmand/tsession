package cmd

import (
	"reflect"
	"testing"
)

func TestSplitDashDash(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantBefore   []string
		wantAfter    []string
	}{
		{"no dashdash", []string{"branch"}, []string{"branch"}, nil},
		{"with dashdash", []string{"branch", "--", "--resume"}, []string{"branch"}, []string{"--resume"}},
		{"dashdash only", []string{"--"}, []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := splitDashDash(tc.args)
			if !reflect.DeepEqual(before, tc.wantBefore) || !reflect.DeepEqual(after, tc.wantAfter) {
				t.Fatalf("got (%v,%v), want (%v,%v)", before, after, tc.wantBefore, tc.wantAfter)
			}
		})
	}
}

func TestValidateNewArgs(t *testing.T) {
	if err := validateNewArgs("", ""); err != nil {
		t.Errorf("neither branch nor path (defaults to cwd): unexpected error %v", err)
	}
	if err := validateNewArgs("b", "/p"); err == nil {
		t.Error("expected error when both branch and path given")
	}
	if err := validateNewArgs("b", ""); err != nil {
		t.Errorf("branch only: unexpected error %v", err)
	}
	if err := validateNewArgs("", "/p"); err != nil {
		t.Errorf("path only: unexpected error %v", err)
	}
}

func TestParseNewArgs(t *testing.T) {
	cases := []struct {
		name        string
		before      []string
		wantBranch  string
		wantPath    string
		wantCmd     string
		wantVerbose bool
		wantErr     bool
	}{
		{"branch only", []string{"feat"}, "feat", "", "", false, false},
		{"long path", []string{"--path", "/tmp/wt"}, "", "/tmp/wt", "", false, false},
		{"short path", []string{"-p", "/tmp/wt"}, "", "/tmp/wt", "", false, false},
		{"no args defaults to cwd", nil, "", ".", "", false, false},
		{"verbose short", []string{"-v", "feat"}, "feat", "", "", true, false},
		{"verbose long", []string{"--verbose", "feat"}, "feat", "", "", true, false},
		{"cmd override", []string{"--cmd", "pi --resume", "feat"}, "feat", "", "pi --resume", false, false},
		{"both branch and path", []string{"-p", "/tmp/wt", "feat"}, "", "", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			branch, path, cmd, verbose, err := parseNewArgs(tc.before)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if branch != tc.wantBranch || path != tc.wantPath {
				t.Fatalf("got (%q,%q), want (%q,%q)", branch, path, tc.wantBranch, tc.wantPath)
			}
			if cmd != tc.wantCmd {
				t.Fatalf("cmd: got %q, want %q", cmd, tc.wantCmd)
			}
			if verbose != tc.wantVerbose {
				t.Fatalf("verbose: got %t, want %t", verbose, tc.wantVerbose)
			}
		})
	}
}

func TestBuildAgentCommand(t *testing.T) {
	if got := buildAgentCommand("copilot", nil); got != "copilot" {
		t.Errorf("got %q, want copilot", got)
	}
	if got := buildAgentCommand("agency copilot --hub", []string{"--resume", "x y"}); got != "agency copilot --hub '--resume' 'x y'" {
		t.Errorf("got %q", got)
	}
}
