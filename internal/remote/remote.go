package remote

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

//go:embed gather.bash
var gatherScript string

const defaultCopilotDir = "~/.copilot"

type GatherSession struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	Repository string `json:"repository"`
	Summary    string `json:"summary"`
	UpdatedAt  string `json:"updated_at"`
}

type GatherStateDir struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	EventsTail string `json:"events_tail"`
	PID        int    `json:"pid"`
}

type GatherTmuxSession struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type GatherTmuxPane struct {
	SessionName string `json:"session_name"`
	WindowIndex string `json:"window_index"`
	PaneIndex   string `json:"pane_index"`
	PID         int    `json:"pid"`
}

type GatherResult struct {
	Error        string              `json:"error,omitempty"`
	Sessions     []GatherSession     `json:"sessions"`
	StateDirs    []GatherStateDir    `json:"state_dirs"`
	TmuxSessions []GatherTmuxSession `json:"tmux_sessions"`
	TmuxPanes    []GatherTmuxPane    `json:"tmux_panes"`
	ProcessTree  map[int]int         `json:"process_tree"`
}

func ParseGatherOutput(data []byte) (*GatherResult, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("parse gather output: empty output")
	}
	var gr GatherResult
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, fmt.Errorf("parse gather output: %w", err)
	}
	if gr.Error != "" {
		return nil, fmt.Errorf("remote error: %s", gr.Error)
	}
	if gr.ProcessTree == nil {
		gr.ProcessTree = map[int]int{}
	}
	return &gr, nil
}

func Fetch(ctx context.Context, remote config.Remote, maxAge time.Duration) (*GatherResult, error) {
	copilotDir := remote.CopilotDir
	if copilotDir == "" {
		copilotDir = defaultCopilotDir
	}
	hours := maxAgeHours(maxAge)
	remoteCmd := fmt.Sprintf("bash -s -- %s %d", shellQuote(copilotDir), hours)

	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", remote.Host, remoteCmd)
	cmd.Stdin = strings.NewReader(gatherScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("ssh %s: %w: %s", remote.Host, err, msg)
		}
		return nil, fmt.Errorf("ssh %s: %w", remote.Host, err)
	}
	return ParseGatherOutput(stdout.Bytes())
}

func FetchAll(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration) (map[string][]sessions.Session, []string) {
	out := make(map[string][]sessions.Session, len(remotes))
	if len(remotes) == 0 {
		return out, nil
	}
	warningsByIndex := make([]string, len(remotes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, remote := range remotes {
		wg.Add(1)
		go func(i int, remote config.Remote) {
			defer wg.Done()
			fetchCtx := ctx
			cancel := func() {}
			if timeout > 0 {
				fetchCtx, cancel = context.WithTimeout(ctx, timeout)
			}
			defer cancel()

			gr, err := Fetch(fetchCtx, remote, maxAge)
			if err != nil {
				warningsByIndex[i] = fmt.Sprintf("remote %s: %v", remote.Name, err)
				return
			}
			sess := gr.ToSessions(remote.Name, maxAge)
			mu.Lock()
			out[remote.Name] = sess
			mu.Unlock()
		}(i, remote)
	}
	wg.Wait()

	warnings := make([]string, 0, len(warningsByIndex))
	for _, warning := range warningsByIndex {
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return out, warnings
}

func (gr *GatherResult) ToSessions(origin string, maxAge time.Duration) []sessions.Session {
	if gr == nil {
		return nil
	}
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	store := make([]sessions.Session, 0, len(gr.Sessions))
	for _, gs := range gr.Sessions {
		s := sessions.Session{
			ID:         gs.ID,
			CWD:        gs.CWD,
			Repository: gs.Repository,
			Summary:    gs.Summary,
			UpdatedAt:  parseTime(gs.UpdatedAt),
			Origin:     origin,
		}
		if !cutoff.IsZero() && !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(cutoff) {
			continue
		}
		store = append(store, s)
	}

	stateDirs := make([]sessions.StateDirInfo, 0, len(gr.StateDirs))
	for _, sd := range gr.StateDirs {
		state, lastEventAt := classifyEventsTail(sd.EventsTail, sd.PID)
		stateDirs = append(stateDirs, sessions.StateDirInfo{
			ID:          sd.ID,
			State:       state,
			LastEventAt: lastEventAt,
			CWD:         sd.CWD,
			PID:         sd.PID,
		})
	}

	tmuxSessions := make([]tmux.Session, 0, len(gr.TmuxSessions))
	for _, ts := range gr.TmuxSessions {
		tmuxSessions = append(tmuxSessions, tmux.Session{Name: ts.Name, Path: ts.Path})
	}
	merged := sessions.MergeRemote(store, stateDirs, tmuxSessions, gr.ProcessTree)

	panes := make([]tmux.Pane, 0, len(gr.TmuxPanes))
	for _, pane := range gr.TmuxPanes {
		panes = append(panes, tmux.Pane{SessionName: pane.SessionName, WindowIndex: pane.WindowIndex, PaneIndex: pane.PaneIndex, PID: pane.PID})
	}
	return sessions.ResolveTmuxByPIDWithTree(merged, stateDirs, panes, gr.ProcessTree)
}

type tailEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Data      struct {
		ToolName string `json:"toolName"`
	} `json:"data"`
}

type parsedTailEvent struct {
	Type     string
	ToolName string
	TS       time.Time
}

func classifyEventsTail(eventsTail string, pid int) (sessions.State, time.Time) {
	events := parseEventsTail(eventsTail)
	if len(events) == 0 {
		return sessions.StateUnknown, time.Time{}
	}
	last := events[len(events)-1]
	for _, event := range events {
		if event.Type == "session.shutdown" {
			return sessions.StateExited, last.TS
		}
	}

	completed := 0
	permCompleted := 0
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		switch event.Type {
		case "tool.execution_complete":
			completed++
		case "permission.completed":
			permCompleted++
		case "permission.requested":
			if permCompleted > 0 {
				permCompleted--
				continue
			}
			return sessions.StateWaiting, last.TS
		case "tool.user_requested":
			return sessions.StateWaiting, last.TS
		case "tool.execution_start":
			if completed > 0 {
				completed--
				continue
			}
			if isUserPromptingTool(event.ToolName) {
				return sessions.StateWaiting, last.TS
			}
		}
	}

	var lastStart, lastEnd time.Time
	for _, event := range events {
		switch event.Type {
		case "assistant.turn_start":
			lastStart = event.TS
		case "assistant.turn_end":
			lastEnd = event.TS
		}
	}
	if !lastStart.IsZero() && lastStart.After(lastEnd) {
		return sessions.StateWorking, last.TS
	}
	if last.Type == "assistant.turn_end" {
		if pid > 0 {
			return sessions.StateActiveIdle, last.TS
		}
		return sessions.StateInactiveIdle, last.TS
	}
	return sessions.StateUnknown, last.TS
}

func parseEventsTail(eventsTail string) []parsedTailEvent {
	lines := strings.Split(eventsTail, "\n")
	out := make([]parsedTailEvent, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw tailEvent
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		out = append(out, parsedTailEvent{Type: raw.Type, ToolName: raw.Data.ToolName, TS: parseTime(raw.Timestamp)})
	}
	return out
}

func isUserPromptingTool(name string) bool {
	switch name {
	case "ask_user", "ask_question", "request_permission":
		return true
	default:
		return false
	}
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func maxAgeHours(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 336
	}
	hours := int(maxAge / time.Hour)
	if maxAge%time.Hour != 0 {
		hours++
	}
	if hours < 1 {
		hours = 1
	}
	return hours
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
