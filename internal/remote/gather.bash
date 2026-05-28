#!/usr/bin/env bash
set -euo pipefail

COPILOT_DIR="${1:-~/.copilot}"
MAX_AGE_HOURS="${2:-336}"

case "$COPILOT_DIR" in
  ~) COPILOT_DIR="$HOME" ;;
  ~/*) COPILOT_DIR="$HOME/${COPILOT_DIR#~/}" ;;
esac

DB="$COPILOT_DIR/session-store.db"
STATE_ROOT="$COPILOT_DIR/session-state"

if ! command -v sqlite3 >/dev/null 2>&1; then
  printf '{"error":"sqlite3 not found"}\n'
  exit 0
fi

json_string() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "${1-}" | jq -Rsa .
  elif command -v python3 >/dev/null 2>&1; then
    python3 - "$1" <<'PY'
import json, sys
print(json.dumps(sys.argv[1]))
PY
  else
    local s="${1-}"
    s=${s//\\/\\\\}
    s=${s//"/\\"}
    s=${s//$'\n'/\\n}
    s=${s//$'\r'/\\r}
    s=${s//$'\t'/\\t}
    printf '"%s"\n' "$s"
  fi
}

encode_object() {
  local kind="$1"
  shift
  case "$kind" in
    state)
      local id="$1" cwd="$2" events_tail="$3" pid="$4"
      printf '{"id":%s,"cwd":%s,"events_tail":%s,"pid":%s}' \
        "$(json_string "$id")" "$(json_string "$cwd")" "$(json_string "$events_tail")" "$pid"
      ;;
    tmux_session)
      local name="$1" path="$2"
      printf '{"name":%s,"path":%s}' "$(json_string "$name")" "$(json_string "$path")"
      ;;
    tmux_pane)
      local session_name="$1" window_index="$2" pane_index="$3" pid="$4"
      printf '{"session_name":%s,"window_index":%s,"pane_index":%s,"pid":%s}' \
        "$(json_string "$session_name")" "$(json_string "$window_index")" "$(json_string "$pane_index")" "$pid"
      ;;
  esac
}

extract_ids() {
  if command -v jq >/dev/null 2>&1; then
    jq -r '.[].id // empty' <<<"$1"
  elif command -v python3 >/dev/null 2>&1; then
    python3 - "$1" <<'PY'
import json, sys
for item in json.loads(sys.argv[1] or '[]'):
    ident = item.get('id')
    if ident:
        print(ident)
PY
  else
    grep -o '"id":"[^"]*"' <<<"$1" | cut -d'"' -f4
  fi
}

if [[ ! -f "$DB" ]]; then
  printf '{"sessions":[],"state_dirs":[],"tmux_sessions":[],"tmux_panes":[],"process_tree":{}}\n'
  exit 0
fi

SESSIONS_JSON="$(sqlite3 -json "$DB" "
SELECT id,
       COALESCE(cwd, '') AS cwd,
       COALESCE(repository, '') AS repository,
       COALESCE(summary, '') AS summary,
       updated_at
FROM sessions
WHERE updated_at >= datetime('now', '-${MAX_AGE_HOURS} hours')
ORDER BY updated_at DESC;
" 2>/dev/null || printf '[]')"

if [[ -z "$SESSIONS_JSON" || ${SESSIONS_JSON:0:1} != '[' ]]; then
  SESSIONS_JSON='[]'
fi

STATE_DIRS_JSON='['
state_first=1
while IFS= read -r id; do
  [[ -n "$id" ]] || continue
  dir="$STATE_ROOT/$id"
  [[ -d "$dir" ]] || continue

  cwd=""
  if [[ -f "$dir/workspace.yaml" ]]; then
    while IFS= read -r line; do
      case "$line" in
        cwd:*)
          cwd="${line#cwd:}"
          cwd="${cwd#${cwd%%[![:space:]]*}}"
          cwd="${cwd%${cwd##*[![:space:]]}}"
          cwd="${cwd#\"}"
          cwd="${cwd%\"}"
          cwd="${cwd#\'}"
          cwd="${cwd%\'}"
          break
          ;;
      esac
    done < "$dir/workspace.yaml"
  fi

  events_tail=""
  if [[ -f "$dir/events.jsonl" ]]; then
    events_tail="$(tail -c 65536 "$dir/events.jsonl" 2>/dev/null || true)"
  fi

  pid=0
  shopt -s nullglob
  for lockfile in "$dir"/inuse.*.lock; do
    base=$(basename "$lockfile")
    pid_part=${base#inuse.}
    pid_part=${pid_part%.lock}
    if [[ "$pid_part" =~ ^[0-9]+$ ]]; then
      pid=$pid_part
      break
    fi
  done
  shopt -u nullglob

  obj="$(encode_object state "$id" "$cwd" "$events_tail" "$pid")"
  if [[ $state_first -eq 1 ]]; then
    state_first=0
  else
    STATE_DIRS_JSON+=','
  fi
  STATE_DIRS_JSON+="$obj"
done < <(extract_ids "$SESSIONS_JSON")
STATE_DIRS_JSON+=']'

TMUX_SESSIONS_JSON='[]'
TMUX_PANES_JSON='[]'
if command -v tmux >/dev/null 2>&1; then
  tmux_sessions_raw="$(tmux list-sessions -F '#{session_name}|#{session_path}' 2>/dev/null || true)"
  if [[ -n "$tmux_sessions_raw" ]]; then
    TMUX_SESSIONS_JSON='['
    first=1
    while IFS='|' read -r name path; do
      [[ -n "$name" ]] || continue
      obj="$(encode_object tmux_session "$name" "$path")"
      if [[ $first -eq 1 ]]; then
        first=0
      else
        TMUX_SESSIONS_JSON+=','
      fi
      TMUX_SESSIONS_JSON+="$obj"
    done <<< "$tmux_sessions_raw"
    TMUX_SESSIONS_JSON+=']'
  fi

  tmux_panes_raw="$(tmux list-panes -a -F '#{session_name}|#{window_index}|#{pane_index}|#{pane_pid}' 2>/dev/null || true)"
  if [[ -n "$tmux_panes_raw" ]]; then
    TMUX_PANES_JSON='['
    first=1
    while IFS='|' read -r session_name window_index pane_index pid; do
      [[ -n "$session_name" && "$pid" =~ ^[0-9]+$ ]] || continue
      obj="$(encode_object tmux_pane "$session_name" "$window_index" "$pane_index" "$pid")"
      if [[ $first -eq 1 ]]; then
        first=0
      else
        TMUX_PANES_JSON+=','
      fi
      TMUX_PANES_JSON+="$obj"
    done <<< "$tmux_panes_raw"
    TMUX_PANES_JSON+=']'
  fi
fi

PROCESS_TREE_JSON='{}'
ps_raw="$(ps -A -o pid=,ppid= 2>/dev/null || true)"
if [[ -n "$ps_raw" ]]; then
  PROCESS_TREE_JSON='{'
  first=1
  while read -r pid ppid; do
    [[ "$pid" =~ ^[0-9]+$ && "$ppid" =~ ^[0-9]+$ ]] || continue
    if [[ $first -eq 1 ]]; then
      first=0
    else
      PROCESS_TREE_JSON+=','
    fi
    PROCESS_TREE_JSON+="\"$pid\":$ppid"
  done <<< "$ps_raw"
  PROCESS_TREE_JSON+='}'
fi

printf '{"sessions":%s,"state_dirs":%s,"tmux_sessions":%s,"tmux_panes":%s,"process_tree":%s}\n' \
  "$SESSIONS_JSON" "$STATE_DIRS_JSON" "$TMUX_SESSIONS_JSON" "$TMUX_PANES_JSON" "$PROCESS_TREE_JSON"
