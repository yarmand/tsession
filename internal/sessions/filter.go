package sessions

// FilterActive keeps only sessions that are attached to a tmux session and
// whose state is something other than exited/unknown/inactive-idle — i.e.
// sessions a user can resume right now and that have meaningful liveness
// signal. For remote sessions (Origin != ""), tmux attachment is not
// required since the remote tmux server may not be reachable via SSH.
//
// This is shared by the CLI's `--active` flag (cmd/list.go, cmd/browse.go)
// and the web UI's session list, which applies the same filter.
func FilterActive(in []Session) []Session {
	out := in[:0:0]
	for _, s := range in {
		if s.Origin == "" && s.TmuxName == "" {
			continue
		}
		if s.State == StateExited || s.State == StateUnknown || s.State == StateInactiveIdle {
			continue
		}
		out = append(out, s)
	}
	return out
}
