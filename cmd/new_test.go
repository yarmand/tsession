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
	if err := validateNewArgs("", ""); err == nil {
		t.Error("expected error when neither branch nor path given")
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

func TestBuildCopilotCommand(t *testing.T) {
	if got := buildCopilotCommand(nil); got != "copilot" {
		t.Errorf("got %q, want copilot", got)
	}
	if got := buildCopilotCommand([]string{"--resume", "x y"}); got != "copilot '--resume' 'x y'" {
		t.Errorf("got %q", got)
	}
}
