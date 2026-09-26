// tsession web UI client. No build step, no framework: this file is served
// as-is via go:embed and runs directly in the browser.
(() => {
  "use strict";

  const REFRESH_INTERVAL_MS = 5000;
  const SIDEBAR_WIDTH_KEY = "tsession-sidebar-width";
  // Caps how many session terminals stay warm in the browser at once.
  // Evicting a pane only closes its WebSocket — the server-side PTY stays
  // warm (see webterm.Registry) and a later re-attach replays its ring
  // buffer, so this is resource hygiene, not a data-loss risk.
  const PANE_CAP = 8;
  const CODE_WIDTH_KEY = "tsession-code-width";

  const state = {
    sessions: [],
    selectedKey: null, // `${origin}\u0000${id}`
    panes: new Map(), // sessionKey -> { key, el, term, fitAddon, socket, status, connectingEl }, ordered oldest-first (LRU)
    activeKey: null, // sessionKey of the currently visible pane
    renameTarget: null, // { kind: "session"|"repo", id, currentName }
    focusTarget: "list", // "list" | "terminal" — see the Focus management section below
    listIndex: 0, // keyboard-navigation cursor row in state.sessions
    sidebarCollapsed: false,
    sidebarOverlay: false,
    sidebarResizing: false,
    codeResizing: false,
    codeViews: new Map(), // sessionKey -> { s, path, status, error, log, iframe, visible, pollTimer }
  };

  const appEl = document.getElementById("app");
  const sessionsEl = document.getElementById("sessions");
  const sessionsToggle = document.getElementById("sessions-toggle");
  const sessionsResize = document.getElementById("sessions-resize");
  const listEl = document.getElementById("session-list");
  const infoEl = document.getElementById("session-info");
  const terminalEl = document.getElementById("terminal");
  const emptyStateEl = document.getElementById("empty-state");
  const bannerEl = document.getElementById("terminal-banner");
  const renameModal = document.getElementById("rename-modal");
  const renameInput = document.getElementById("rename-input");
  const renameTitle = document.getElementById("rename-modal-title");
  const renameError = document.getElementById("rename-error");
  const codeResize = document.getElementById("code-resize");
  const codePane = document.getElementById("code-pane");
  const codePaneLabel = document.getElementById("code-pane-label");
  const codePaneStatus = document.getElementById("code-pane-status");
  const codePaneClose = document.getElementById("code-pane-close");
  const codePaneBody = document.getElementById("code-pane-body");
  const codePlaceholder = document.getElementById("code-pane-placeholder");

  function sessionKey(s) {
    return (s.origin || "") + "\u0000" + s.id;
  }

  function glyphClass(stateStr) {
    switch (stateStr) {
      case "working": return "working";
      case "question": return "question";
      case "done": return "done";
      case "active": return "active";
      default: return "idle";
    }
  }

  function glyphChar(stateStr) {
    switch (stateStr) {
      case "working": return "\u25CF"; // ●
      case "question": return "\u25D0"; // ◐
      case "done": return "\u2713"; // ✓
      case "active": return "\u25CB"; // ○
      default: return "\u00B7"; // ·
    }
  }

  function sourceGlyph(source) {
    return source === "pi" ? "\u03C0" : "\u00A9"; // π / ©
  }

  function remoteColors() {
    const origins = [...new Set(
      state.sessions.filter((s) => s.origin).map((s) => s.origin)
    )].sort();
    const colors = new Map();
    origins.forEach((origin, index) => {
      const hue = Math.round((index * 137.508 + 24) % 360);
      colors.set(origin, `hsl(${hue} 78% 68%)`);
    });
    return colors;
  }

  function formatAge(iso) {
    if (!iso) return "";
    const then = new Date(iso).getTime();
    if (Number.isNaN(then)) return "";
    const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000));
    if (seconds < 60) return seconds + "s";
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return minutes + "m";
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return hours + "h";
    const days = Math.floor(hours / 24);
    if (days < 7) return days + "d";
    return Math.floor(days / 7) + "w";
  }

  // worktreeTooltip explains what the leading row token actually is. A
  // custom name replaces the worktree folder in the row, so the tooltip is
  // the only place the folder still shows up at a glance.
  function worktreeTooltip(s) {
    const parts = [];
    if (s.name) parts.push("Name: " + s.name);
    if (s.worktree) parts.push("Worktree: " + s.worktree);
    if (s.cwd) parts.push(s.cwd);
    return parts.join("\n");
  }

  // Go marshals an unset time.Time as "0001-01-01T00:00:00Z", which is a
  // non-empty (truthy) string — so `lastEventAt || updatedAt` would pick the
  // zero value and render a nonsense age. Treat anything before 1971 as
  // unset.
  function sessionTimestamp(s) {
    const usable = (iso) => {
      if (!iso) return false;
      const t = new Date(iso).getTime();
      return !Number.isNaN(t) && t > 31536000000;
    };
    if (usable(s.lastEventAt)) return s.lastEventAt;
    if (usable(s.updatedAt)) return s.updatedAt;
    return "";
  }

  function renderSessions() {
    listEl.innerHTML = "";
    const colors = remoteColors();
    state.sessions.forEach((s, i) => {
      const key = sessionKey(s);
      const li = document.createElement("li");
      let cls = "session-row";
      if (key === state.selectedKey) cls += " selected";
      if (state.focusTarget === "list" && i === state.listIndex) cls += " cursor";
      li.className = cls;
      li.dataset.key = key;

      const line1 = document.createElement("div");
      line1.className = "line1";

      const glyph = document.createElement("span");
      glyph.className = "glyph " + glyphClass(s.state);
      glyph.textContent = glyphChar(s.state);
      line1.appendChild(glyph);

      const src = document.createElement("span");
      src.textContent = sourceGlyph(s.source);
      line1.appendChild(src);

      const location = document.createElement("span");
      const remote = Boolean(s.origin);
      location.className = "location " + (remote ? "remote" : "local");
      location.textContent = remote ? (s.remoteHost || s.origin) : "local";
      if (remote) location.style.color = colors.get(s.origin);
      location.title = remote
        ? "Remote session: " + (s.remoteHost || s.origin)
        : "Local session";
      line1.appendChild(location);

      // Several sessions often share one repository but live in different
      // worktrees, so the worktree folder (or the session's custom name)
      // leads the row — it is what actually tells them apart.
      const worktree = document.createElement("span");
      worktree.className = "worktree" + (s.name ? " named" : "");
      worktree.textContent = s.name || s.worktree || s.id;
      worktree.title = worktreeTooltip(s);
      line1.appendChild(worktree);

      const repo = document.createElement("span");
      repo.className = "repo";
      repo.textContent = s.repository || "";
      repo.title = "Repository " + (s.repository || "") +
        " \u2014 right-click (or ctrl-a with the list focused) to set an alias.";
      // Mirrors --short rendering: when the repository label and the
      // worktree name are the same word, showing it twice just steals room
      // from the token that distinguishes sessions.
      if (s.repository && s.repository !== worktree.textContent) {
        line1.appendChild(repo);
      }

      const age = document.createElement("span");
      age.className = "age";
      age.textContent = formatAge(sessionTimestamp(s));
      line1.appendChild(age);

      li.appendChild(line1);

      const summary = document.createElement("div");
      summary.className = "summary";
      summary.textContent = s.summary || "";
      li.appendChild(summary);

      li.addEventListener("click", () => {
        state.listIndex = i;
        selectSession(s);
      });
      li.addEventListener("dblclick", () => openRenameModal("session", s.id, s.name || ""));
      repo.addEventListener("contextmenu", (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        if (!s.repositoryId) return;
        openRenameModal("repo", s.repositoryId, s.repository || "");
      });
      listEl.appendChild(li);
    });
    renderSessionInfo();
  }

  function infoValue(value, fallback = "\u2014") {
    return value || fallback;
  }

  function addInfoRow(label, value, className = "") {
    const row = document.createElement("div");
    row.className = "info-row" + (className ? " " + className : "");
    const labelEl = document.createElement("span");
    labelEl.className = "info-label";
    labelEl.textContent = label;
    const valueEl = document.createElement("span");
    valueEl.className = "info-value";
    valueEl.textContent = value;
    valueEl.title = value;
    row.append(labelEl, valueEl);
    infoEl.appendChild(row);
  }

  function renderSessionInfo() {
    infoEl.innerHTML = "";
    const s = state.sessions[state.listIndex];
    if (!s) {
      const empty = document.createElement("div");
      empty.className = "info-empty";
      empty.textContent = "Select a session to inspect.";
      infoEl.appendChild(empty);
      return;
    }

    addInfoRow("ID", infoValue(s.id));
    addInfoRow("State", infoValue(s.state));
    addInfoRow("Age", infoValue(formatAge(sessionTimestamp(s))));
    addInfoRow("Name", infoValue(s.name, "(unnamed)"));
    addInfoRow("Worktree", infoValue(s.worktree));
    addInfoRow("CWD", infoValue(s.cwd));
    addInfoRow("Repo", infoValue(s.repository));
    addInfoRow("Source", infoValue(s.source));
    addInfoRow("Remote", s.origin ? infoValue(s.remoteHost || s.origin) : "local");
    addInfoRow("Tmux", infoValue(s.tmuxSession, "(none)"));
    if (s.tmuxTarget && s.tmuxTarget !== s.tmuxSession) {
      addInfoRow("Target", s.tmuxTarget);
    }
    addInfoRow("Summary", infoValue(s.summary, "(no summary)"), "info-summary");
  }

  async function refreshSessions() {
    try {
      const resp = await fetch("/api/sessions");
      if (!resp.ok) return;
      const data = await resp.json();
      state.sessions = data.sessions || [];
      if (state.listIndex == null) state.listIndex = 0;
      if (state.listIndex >= state.sessions.length) {
        state.listIndex = Math.max(0, state.sessions.length - 1);
      }
      renderSessions();
    } catch (e) {
      // Transient fetch failures are expected during server restarts; the
      // next poll retries.
    }
  }

  function wsScheme() {
    return location.protocol === "https:" ? "wss:" : "ws:";
  }

  // Maps a KeyboardEvent's physical `code` to the base character it would
  // produce on a standard US keyboard layout, independent of modifier keys.
  // Used to build Alt/Option-chord escape sequences ourselves (see
  // altEscapeSequence) because macOS composes Option+key into a special
  // unicode character (e.g. Alt+, -> "≤") at the OS level before the
  // browser ever sees a plain keystroke — `event.key` reports the composed
  // character, but `event.code` still reliably identifies the physical key
  // regardless of what Option composed, so we reconstruct the intended
  // character from `code` instead of trusting `key`.
  const CODE_TO_BASE_CHAR = {
    Backquote: ["`", "~"], Minus: ["-", "_"], Equal: ["=", "+"],
    BracketLeft: ["[", "{"], BracketRight: ["]", "}"], Backslash: ["\\", "|"],
    Semicolon: [";", ":"], Quote: ["'", '"'], Comma: [",", "<"],
    Period: [".", ">"], Slash: ["/", "?"], Space: [" ", " "],
    Digit0: ["0", ")"], Digit1: ["1", "!"], Digit2: ["2", "@"], Digit3: ["3", "#"],
    Digit4: ["4", "$"], Digit5: ["5", "%"], Digit6: ["6", "^"], Digit7: ["7", "&"],
    Digit8: ["8", "*"], Digit9: ["9", "("],
  };

  // altEscapeSequence returns the ESC-prefixed byte sequence a real terminal
  // would send for an Option/Alt-chord keydown (e.g. Alt+, -> "\x1b,"), or
  // null if ev isn't a chord this function knows how to translate.
  function altEscapeSequence(ev) {
    if (!ev.altKey || ev.ctrlKey || ev.metaKey) return null;
    let base = null;
    if (/^Key[A-Z]$/.test(ev.code)) {
      const letter = ev.code.slice(3).toLowerCase();
      base = ev.shiftKey ? letter.toUpperCase() : letter;
    } else {
      const pair = CODE_TO_BASE_CHAR[ev.code];
      if (pair) base = ev.shiftKey ? pair[1] : pair[0];
    }
    if (base === null) return null;
    return "\x1b" + base;
  }

  // touchPane moves key to the most-recently-used end of state.panes (a Map
  // preserves insertion order, so re-inserting is enough to track LRU order
  // without a separate timestamp/index).
  function touchPane(key, pane) {
    state.panes.delete(key);
    state.panes.set(key, pane);
  }

  function activePane() {
    return state.activeKey ? state.panes.get(state.activeKey) : null;
  }

  // evictPane tears down a pane's browser-side terminal and socket only.
  // The server-side PTY (webterm.Registry) is untouched, so a later
  // re-attach to the same session replays its ring buffer instead of
  // starting fresh.
  function evictPane(key) {
    const pane = state.panes.get(key);
    if (!pane) return;
    if (pane.socket) {
      try { pane.socket.close(); } catch (e) { /* already closing */ }
    }
    try { pane.term.dispose(); } catch (e) { /* already disposed */ }
    pane.el.remove();
    state.panes.delete(key);
    if (state.activeKey === key) state.activeKey = null;
  }

  function evictLRUIfAtCap() {
    while (state.panes.size >= PANE_CAP) {
      const oldestKey = state.panes.keys().next().value;
      if (oldestKey === undefined) return;
      evictPane(oldestKey);
    }
  }

  function updatePaneStatus(pane, status) {
    pane.status = status;
    pane.connectingEl.classList.toggle("hidden", status !== "connecting");
  }

  // ensurePane returns the pane for key, creating (and, if the LRU cap is
  // full, evicting the oldest other pane) one on first use. It never
  // touches the network — connectPane does that separately — so switching
  // back to an already-open pane is just a DOM show/hide plus an xterm
  // focus, with no socket churn.
  function ensurePane(key) {
    const existing = state.panes.get(key);
    if (existing) return existing;

    evictLRUIfAtCap();

    const el = document.createElement("div");
    el.className = "term-pane hidden";
    const termContainer = document.createElement("div");
    termContainer.className = "term-container";
    el.appendChild(termContainer);
    const connectingEl = document.createElement("div");
    connectingEl.className = "term-connecting hidden";
    connectingEl.textContent = "Connecting\u2026";
    el.appendChild(connectingEl);
    terminalEl.appendChild(el);

    const term = new Terminal({
      convertEol: true,
      cursorBlink: true,
      fontSize: 13.5,
      // Courier New (xterm.js's default fallback) renders noticeably
      // jagged at small sizes on macOS. SF Mono is the system's well-hinted
      // monospace font; Menlo/Monaco/Consolas/Liberation Mono are fallbacks
      // for platforms where SF Mono isn't installed.
      fontFamily:
        "'SF Mono', Menlo, Monaco, Consolas, 'Liberation Mono', monospace",
      theme: { background: "#000000" },
    });
    const fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
    term.open(termContainer);

    // pane is assigned below, but these closures capture the *variable*
    // (not its current value), so they safely see the finished object by
    // the time the user can trigger them.
    let pane;

    const encoder = new TextEncoder();
    term.onData((data) => {
      if (pane.socket && pane.socket.readyState === WebSocket.OPEN) {
        // WebSocket.send(string) always sends a TEXT frame, but the server
        // only treats keystrokes as PTY input on BINARY frames (TEXT frames
        // are parsed as JSON control messages, e.g. resize) — so keystrokes
        // must be sent as bytes, not as a string.
        pane.socket.send(encoder.encode(data));
      }
    });
    term.onResize(() => {
      if (pane.key === state.activeKey) sendResize(pane);
    });

    // xterm.js only routes keydown to the PTY when its own hidden textarea
    // has real DOM focus. attachCustomKeyEventHandler runs before xterm's
    // internal handling; stopping propagation here keeps page-level
    // shortcuts (rename modal Escape, etc.) from also reacting to keys the
    // user is sending to the terminal.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type === "keydown") {
        const seq = altEscapeSequence(ev);
        if (seq !== null) {
          // Send the escape sequence ourselves and prevent the browser's
          // default action, which is what would otherwise insert macOS's
          // composed special character (e.g. "≤" for Alt+,) — xterm.js's
          // own macOptionIsMeta handling only inspects the already-composed
          // `event.key`/`keyCode`, so it can't reliably recover the
          // original key for punctuation. Returning false tells xterm not
          // to process this event any further itself.
          ev.preventDefault();
          if (pane.socket && pane.socket.readyState === WebSocket.OPEN) {
            pane.socket.send(encoder.encode(seq));
          }
          ev.stopPropagation();
          return false;
        }
      }
      ev.stopPropagation();
      return true; // let xterm handle it normally
    });

    pane = { key, el, term, fitAddon, socket: null, status: "connecting", connectingEl };
    state.panes.set(key, pane);
    return pane;
  }

  // connectPane opens (or reopens, e.g. via the banner's Retry button) pane's
  // WebSocket. It is a no-op while a socket is already open or connecting,
  // so reselecting an already-live session never churns the network.
  function connectPane(pane, s) {
    if (pane.socket) return;
    updatePaneStatus(pane, "connecting");

    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    const url = wsScheme() + "//" + location.host + "/api/terminal/" + originSegment + "/" + encodeURIComponent(s.id);
    const socket = new WebSocket(url);
    socket.binaryType = "arraybuffer";
    pane.socket = socket;

    socket.addEventListener("open", () => {
      updatePaneStatus(pane, "open");
      if (pane.key === state.activeKey) {
        sendResize(pane);
        focusTerminal();
      }
    });
    socket.addEventListener("message", (ev) => {
      if (typeof ev.data === "string") {
        pane.term.write(ev.data);
      } else {
        pane.term.write(new Uint8Array(ev.data));
      }
    });
    socket.addEventListener("close", (ev) => {
      if (pane.socket === socket) {
        pane.socket = null;
        updatePaneStatus(pane, "closed");
        if (pane.key === state.activeKey) {
          showBanner("Connection closed" + (ev.reason ? ": " + ev.reason : "") + ".");
        }
      }
    });
    socket.addEventListener("error", () => {
      if (pane.socket === socket) {
        updatePaneStatus(pane, "error");
        reportFailure("terminal-connect-failed", s);
        if (pane.key === state.activeKey) {
          showBanner("Failed to connect to session.");
        }
      }
    });
  }

  // showPane hides the previously active pane (if any) and reveals key's,
  // without resetting or re-replaying its contents — this is what preserves
  // scrollback and viewport position across switches. Fitting only ever
  // happens for the pane that is actually visible; a hidden pane has zero
  // layout size and would compute a garbage geometry.
  function showPane(key) {
    const pane = state.panes.get(key);
    if (state.activeKey && state.activeKey !== key) {
      const prev = state.panes.get(state.activeKey);
      if (prev) prev.el.classList.add("hidden");
    }
    state.activeKey = key;
    pane.el.classList.remove("hidden");
    touchPane(key, pane);
    requestAnimationFrame(() => pane.fitAddon.fit());
  }

  function showBanner(message) {
    bannerEl.innerHTML = "";
    const text = document.createElement("span");
    text.textContent = message;
    bannerEl.appendChild(text);
    const retry = document.createElement("button");
    retry.textContent = "Retry";
    retry.addEventListener("click", () => {
      bannerEl.classList.add("hidden");
      if (state.selectedSession) selectSession(state.selectedSession);
    });
    bannerEl.appendChild(retry);
    bannerEl.classList.remove("hidden");
  }

  function reportUserInteraction(action, s, detail = "", level = "info") {
    const payload = {
      action,
      sessionId: s && s.id ? s.id : "",
      origin: s && s.origin ? s.origin : "",
      sessionName: s ? displayName(s) : "",
      detail,
      level,
    };
    const body = JSON.stringify(payload);
    try {
      if (navigator.sendBeacon) {
        const blob = new Blob([body], { type: "application/json" });
        if (navigator.sendBeacon("/api/debug", blob)) return;
      }
      fetch("/api/debug", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
        keepalive: true,
      }).catch(() => {});
    } catch (e) {
      // Debug logging must never block the UI.
    }
  }

  // reportFailure is reportUserInteraction's "error" counterpart: any
  // failure the client observes on its own (a code view failing to start,
  // a poll erroring out, a terminal socket dying, ...) is reported the same
  // way, so `grep level=error` in the serve log finds it regardless of
  // whether the server or the browser detected it first.
  function reportFailure(action, s, detail = "") {
    reportUserInteraction(action, s, detail, "error");
  }

  function selectSession(s) {
    reportUserInteraction("select-session", s);
    if (state.sidebarCollapsed) {
      state.sidebarOverlay = false;
      updateSidebarState();
    }
    state.selectedSession = s;
    state.selectedKey = sessionKey(s);
    const idx = state.sessions.findIndex((x) => sessionKey(x) === state.selectedKey);
    if (idx >= 0) state.listIndex = idx;
    renderSessions();
    renderCodePane();

    emptyStateEl.classList.add("hidden");
    terminalEl.classList.remove("hidden");
    bannerEl.classList.add("hidden");

    const pane = ensurePane(state.selectedKey);
    showPane(state.selectedKey);
    connectPane(pane, s);
    if (pane.status === "open") focusTerminal();
  }

  function sendResize(pane) {
    if (!pane || !pane.socket || pane.socket.readyState !== WebSocket.OPEN) return;
    const msg = JSON.stringify({ type: "resize", cols: pane.term.cols, rows: pane.term.rows });
    pane.socket.send(msg);
  }

  // --- Code view: an optional VS Code (code serve-web) pane docked next to
  // the terminal, toggled with Alt+E while the session list is focused. It
  // is scoped per session: state.codeViews holds one entry per session key
  // that has ever been activated in this tab, each owning at most one
  // retained <iframe> (never destroyed while its entry exists, only shown
  // or hidden) plus the launch/poll bookkeeping for its backing
  // codeserver.Instance. Switching sessions never stops anything server
  // side — only the explicit close button (DELETE) does that.

  function codeServerURL(s) {
    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    return "/api/codeserver/" + originSegment + "/" + encodeURIComponent(s.id);
  }

  function getOrCreateCodeView(s) {
    const key = sessionKey(s);
    let cv = state.codeViews.get(key);
    if (!cv) {
      cv = { s, path: null, status: "stopped", error: "", log: "", iframe: null, visible: false, pollTimer: null };
      state.codeViews.set(key, cv);
    }
    cv.s = s;
    return cv;
  }

  function stopPolling(cv) {
    if (cv.pollTimer) {
      clearTimeout(cv.pollTimer);
      cv.pollTimer = null;
    }
  }

  function pollCodeStatus(cv) {
    stopPolling(cv);
    cv.pollTimer = setTimeout(async () => {
      cv.pollTimer = null;
      try {
        const resp = await fetch(codeServerURL(cv.s));
        const data = await resp.json();
        applyCodeStatus(cv, data);
      } catch (e) {
        cv.status = "failed";
        cv.error = String(e);
        reportFailure("code-view-poll-failed", cv.s, cv.error);
        renderCodePane();
      }
      if (cv.status === "starting") pollCodeStatus(cv);
    }, 1500);
  }

  function applyCodeStatus(cv, data) {
    const wasFailed = cv.status === "failed";
    cv.status = data.status || "stopped";
    cv.error = data.error || "";
    cv.log = data.log || "";
    if (data.path) cv.path = data.path;
    if (cv.status === "failed" && !wasFailed) {
      reportFailure("code-view-failed", cv.s, cv.error);
    }
    if (cv.status === "running" && cv.path) ensureCodeIframe(cv);
    renderCodePane();
  }

  function ensureCodeIframe(cv) {
    if (cv.iframe) return;
    const iframe = document.createElement("iframe");
    iframe.className = "code-frame hidden";
    iframe.src = cv.path;
    iframe.title = "VS Code";
    codePaneBody.appendChild(iframe);
    cv.iframe = iframe;
  }

  function placeholderMessage(cv) {
    if (cv.status === "failed") {
      return "VS Code failed to start" + (cv.error ? ":\n" + cv.error : ".") + (cv.log ? "\n\n" + cv.log : "");
    }
    if (cv.status === "stopped") return "Code view stopped.";
    return "Starting VS Code\u2026\nThis can take a while on first run." + (cv.log ? "\n\n" + cv.log : "");
  }

  function statusLabel(status) {
    switch (status) {
      case "starting": return "starting\u2026";
      case "running": return "";
      case "failed": return "failed";
      case "stopped": return "stopped";
      default: return "";
    }
  }

  // renderCodePane reconciles the DOM with state.codeViews for whichever
  // session is currently selected: it hides every other session's iframe
  // (they stay in the DOM, just hidden), then shows either the selected
  // session's iframe (once running) or the placeholder (while starting,
  // failed, or stopped) depending on its view's visibility.
  function renderCodePane() {
    const cv = state.selectedKey != null ? state.codeViews.get(state.selectedKey) : null;
    const showPane = !!(cv && cv.visible);

    state.codeViews.forEach((other) => {
      if (other.iframe && other !== cv) other.iframe.classList.add("hidden");
    });

    codePane.classList.toggle("hidden", !showPane);
    codeResize.classList.toggle("hidden", !showPane);

    if (showPane) {
      codePaneLabel.textContent = displayName(state.selectedSession);
      codePaneStatus.textContent = statusLabel(cv.status);
      if (cv.status === "running" && cv.iframe) {
        cv.iframe.classList.remove("hidden");
        codePlaceholder.classList.add("hidden");
      } else {
        if (cv.iframe) cv.iframe.classList.add("hidden");
        codePlaceholder.textContent = placeholderMessage(cv);
        codePlaceholder.classList.remove("hidden");
      }
    } else if (cv && cv.iframe) {
      cv.iframe.classList.add("hidden");
    }

    if (state.fitAddon) requestAnimationFrame(() => state.fitAddon.fit());
  }

  // toggleCodeView is Alt+E's handler: it acts on the highlighted row,
  // selecting it first if it isn't already selected, then toggles that
  // session's code view — starting a code serve-web instance on first
  // activation, or reusing (never relaunching) one that's already running.
  async function toggleCodeView() {
    if (state.listIndex == null) return;
    const s = state.sessions[state.listIndex];
    if (!s) return;
    if (sessionKey(s) !== state.selectedKey) selectSession(s);

    const cv = getOrCreateCodeView(s);
    if (cv.visible) {
      cv.visible = false;
      reportUserInteraction("code-view-hide", s);
      renderCodePane();
      return;
    }

    cv.visible = true;
    reportUserInteraction("code-view-show", s);
    if (cv.status === "running" || cv.status === "starting") {
      renderCodePane();
      if (cv.status === "starting" && !cv.pollTimer) pollCodeStatus(cv);
      return;
    }

    cv.status = "starting";
    cv.error = "";
    renderCodePane();
    try {
      const resp = await fetch(codeServerURL(s), { method: "POST" });
      const data = await resp.json();
      if (!resp.ok) throw new Error(data.error || (await resp.text()) || "failed to start");
      applyCodeStatus(cv, data);
      if (cv.status === "starting") pollCodeStatus(cv);
    } catch (e) {
      cv.status = "failed";
      cv.error = String(e && e.message ? e.message : e);
      reportFailure("code-view-start-failed", s, cv.error);
      renderCodePane();
    }
  }

  async function closeCodeView() {
    if (!state.selectedSession) return;
    const s = state.selectedSession;
    const cv = state.codeViews.get(sessionKey(s));
    if (!cv) return;
    stopPolling(cv);
    cv.visible = false;
    cv.status = "stopped";
    reportUserInteraction("code-view-close", s);
    if (cv.iframe) {
      cv.iframe.remove();
      cv.iframe = null;
    }
    renderCodePane();
    try {
      await fetch(codeServerURL(s), { method: "DELETE" });
    } catch (e) {
      // Best-effort: the pane is already closed client-side regardless.
    }
  }

  // --- Focus management: Alt+/ toggles keyboard focus between the session
  // list and the terminal. Match the physical Slash key because macOS Option
  // may compose event.key into a different character. While the terminal has focus, all
  // keystrokes go to the PTY; while the list has focus, arrow keys move a
  // highlight and Enter attaches. Browser-overridable shortcuts (Cmd+F,
  // Cmd+S, etc.) are suppressed while the terminal is focused so they reach
  // tmux instead — a few shortcuts (Cmd+W/T/N/Q and similar) are reserved by
  // the browser/OS itself and cannot be intercepted from JavaScript.

  function focusTerminal() {
    state.focusTarget = "terminal";
    state.sidebarOverlay = false;
    const pane = activePane();
    if (pane && state.selectedSession) pane.term.focus();
    updateSidebarState();
    updateFocusIndicator();
    reportUserInteraction("focus-terminal", state.selectedSession);
  }

  function focusList() {
    state.focusTarget = "list";
    if (state.sidebarCollapsed) state.sidebarOverlay = true;
    const pane = activePane();
    if (pane) pane.term.blur();
    if (state.listIndex == null) state.listIndex = 0;
    updateSidebarState();
    updateFocusIndicator();
    renderSessions();
    reportUserInteraction("focus-list", state.selectedSession);
  }

  function updateSidebarState() {
    appEl.classList.toggle("sidebar-collapsed", state.sidebarCollapsed);
    appEl.classList.toggle(
      "sidebar-overlay",
      state.sidebarCollapsed && state.sidebarOverlay
    );
    const action = state.sidebarCollapsed ? "Show" : "Hide";
    sessionsToggle.setAttribute("aria-label", action + " session list");
    sessionsToggle.title = action + " session list (Alt+H)";
  }

  function toggleSidebar() {
    state.sidebarCollapsed = !state.sidebarCollapsed;
    state.sidebarOverlay = false;
    reportUserInteraction(state.sidebarCollapsed ? "sidebar-hide" : "sidebar-show", state.selectedSession);
    if (state.sidebarCollapsed && state.selectedSession) {
      focusTerminal();
      return;
    }
    if (!state.sidebarCollapsed) focusList();
    updateSidebarState();
  }

  function setSidebarWidth(px, persist = true) {
    const maxWidth = Math.max(180, window.innerWidth * 0.6);
    const width = Math.round(Math.max(180, Math.min(px, maxWidth)));
    appEl.style.setProperty("--sidebar-width", width + "px");
    if (persist) {
      try {
        localStorage.setItem(SIDEBAR_WIDTH_KEY, String(width));
      } catch (e) {
        // Storage can be unavailable in restricted browser contexts.
      }
    }
  }

  function restoreSidebarWidth() {
    try {
      const width = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
      if (Number.isFinite(width) && width > 0) setSidebarWidth(width, false);
    } catch (e) {
      // Keep the CSS default when storage is unavailable.
    }
  }

  // setCodeWidth/restoreCodeWidth mirror setSidebarWidth/restoreSidebarWidth
  // exactly, but for the code pane on the opposite (right) edge of the
  // window, so its width is computed from the distance between the cursor
  // and the window's right edge rather than its left edge.
  function setCodeWidth(px, persist = true) {
    const maxWidth = Math.max(240, window.innerWidth * 0.85);
    const width = Math.round(Math.max(240, Math.min(px, maxWidth)));
    codePane.style.setProperty("--code-width", width + "px");
    if (persist) {
      try {
        localStorage.setItem(CODE_WIDTH_KEY, String(width));
      } catch (e) {
        // Storage can be unavailable in restricted browser contexts.
      }
    }
  }

  function restoreCodeWidth() {
    try {
      const width = Number(localStorage.getItem(CODE_WIDTH_KEY));
      if (Number.isFinite(width) && width > 0) setCodeWidth(width, false);
    } catch (e) {
      // Keep the CSS default when storage is unavailable.
    }
  }

  function updateFocusIndicator() {
    document.getElementById("sessions").classList.toggle("panel-focused", state.focusTarget === "list");
    document.getElementById("terminal-pane").classList.toggle("panel-focused", state.focusTarget === "terminal");
  }

  function moveListCursor(delta) {
    if (state.sessions.length === 0) return;
    const cur = state.listIndex == null ? 0 : state.listIndex;
    state.listIndex = Math.min(state.sessions.length - 1, Math.max(0, cur + delta));
    renderSessions();
    const row = listEl.children[state.listIndex];
    if (row) row.scrollIntoView({ block: "nearest" });
  }

  function attachHighlighted() {
    if (state.listIndex == null) return;
    const s = state.sessions[state.listIndex];
    if (s) selectSession(s);
  }

  function cursorSession() {
    if (state.listIndex == null) return null;
    return state.sessions[state.listIndex] || null;
  }

  // renameCursorSession/renameCursorRepo mirror the TUI picker's ctrl-n /
  // ctrl-a bindings, acting on the keyboard cursor row.
  function renameCursorSession() {
    const s = cursorSession();
    if (s) openRenameModal("session", s.id, s.name || "");
  }

  function renameCursorRepo() {
    const s = cursorSession();
    if (!s || !s.repositoryId) return;
    openRenameModal("repo", s.repositoryId, s.repository || "");
  }

  const SUPPRESSABLE_KEYS = new Set([
    "f", "s", "p", "g", "k", "l", "o", "d", "u", "j", "e", "h", "n", "1", "2", "3", "4", "5", "6", "7", "8", "9",
  ]);

  document.addEventListener(
    "keydown",
    (ev) => {
      // While the rename modal is open it owns the keyboard (its input has
      // its own Enter/Escape handling); don't let page shortcuts fire too.
      // Escape is still honored here so the modal closes even if focus has
      // drifted off its input.
      if (state.renameTarget) {
        if (ev.key === "Escape") {
          ev.preventDefault();
          ev.stopPropagation();
          closeRenameModal();
        }
        return;
      }

      const key = ev.key.toLowerCase();
      const mod = ev.metaKey || ev.ctrlKey;

      if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "KeyH") {
        ev.preventDefault();
        ev.stopPropagation();
        toggleSidebar();
        return;
      }

      // Alt+/ toggles focus regardless of which panel is currently focused.
      if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "Slash") {
        ev.preventDefault();
        ev.stopPropagation();
        if (state.focusTarget === "terminal") focusList();
        else focusTerminal();
        return;
      }

      // Alt+E toggles the code view for the highlighted session, but only
      // while the session list panel is focused (not while typing into the
      // terminal).
      if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "KeyE") {
        if (state.focusTarget === "list") {
          ev.preventDefault();
          ev.stopPropagation();
          toggleCodeView();
        }
        return;
      }

      if (state.focusTarget === "terminal") {
        // Never intercept plain (unmodified) keys or Cmd/Ctrl+C/V/A/X so
        // native copy/paste/select-all keep working; suppress the rest of
        // the common browser shortcuts so they reach tmux instead. Keys the
        // browser/OS reserves for itself (Cmd+W, Cmd+T, Cmd+N, Cmd+Q, ...)
        // cannot be suppressed by any web page.
        if (mod && SUPPRESSABLE_KEYS.has(key)) {
          ev.preventDefault();
        }
        return;
      }

      if (state.focusTarget === "list") {
        if (ev.ctrlKey && !ev.metaKey && !ev.altKey) {
          if (ev.code === "KeyN") {
            ev.preventDefault();
            ev.stopPropagation();
            renameCursorSession();
            return;
          }
          if (ev.code === "KeyA") {
            ev.preventDefault();
            ev.stopPropagation();
            renameCursorRepo();
            return;
          }
        }
        switch (ev.key) {
          case "ArrowDown":
            ev.preventDefault();
            moveListCursor(1);
            return;
          case "ArrowUp":
            ev.preventDefault();
            moveListCursor(-1);
            return;
          case "Enter":
            ev.preventDefault();
            attachHighlighted();
            return;
        }
      }
    },
    true
  );

  terminalEl.addEventListener("click", focusTerminal);

  // Fitting is deliberately scoped to whichever pane is currently visible:
  // a hidden pane has zero layout size, so fitting it would compute a
  // garbage geometry. Panes that become visible later are fit explicitly
  // by showPane.
  const terminalResizeObserver = new ResizeObserver(() => {
    const pane = activePane();
    if (pane) pane.fitAddon.fit();
  });
  terminalResizeObserver.observe(terminalEl);
  window.addEventListener("resize", () => {
    const pane = activePane();
    if (pane) pane.fitAddon.fit();
  });

  sessionsEl.addEventListener("click", (ev) => {
    if (ev.target.closest(".session-row")) return;
    if (state.focusTarget !== "list") focusList();
  });
  sessionsToggle.addEventListener("click", (ev) => {
    ev.stopPropagation();
    toggleSidebar();
  });
  codePaneClose.addEventListener("click", (ev) => {
    ev.stopPropagation();
    closeCodeView();
  });
  sessionsResize.addEventListener("pointerdown", (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    state.sidebarResizing = true;
    sessionsResize.setPointerCapture(ev.pointerId);
    document.body.classList.add("resizing-sidebar");
  });
  sessionsResize.addEventListener("pointermove", (ev) => {
    if (!state.sidebarResizing) return;
    setSidebarWidth(ev.clientX);
  });
  function stopSidebarResize(ev) {
    if (!state.sidebarResizing) return;
    state.sidebarResizing = false;
    document.body.classList.remove("resizing-sidebar");
    if (sessionsResize.hasPointerCapture(ev.pointerId)) {
      sessionsResize.releasePointerCapture(ev.pointerId);
    }
    reportUserInteraction("sidebar-resize", state.selectedSession, appEl.style.getPropertyValue("--sidebar-width"));
  }
  sessionsResize.addEventListener("pointerup", stopSidebarResize);
  sessionsResize.addEventListener("pointercancel", stopSidebarResize);

  codeResize.addEventListener("pointerdown", (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    state.codeResizing = true;
    codeResize.setPointerCapture(ev.pointerId);
    document.body.classList.add("resizing-code");
  });
  codeResize.addEventListener("pointermove", (ev) => {
    if (!state.codeResizing) return;
    setCodeWidth(window.innerWidth - ev.clientX);
  });
  function stopCodeResize(ev) {
    if (!state.codeResizing) return;
    state.codeResizing = false;
    document.body.classList.remove("resizing-code");
    if (codeResize.hasPointerCapture(ev.pointerId)) {
      codeResize.releasePointerCapture(ev.pointerId);
    }
    reportUserInteraction("code-view-resize", state.selectedSession, codePane.style.getPropertyValue("--code-width"));
  }
  codeResize.addEventListener("pointerup", stopCodeResize);
  codeResize.addEventListener("pointercancel", stopCodeResize);

  // --- Rename modal (sessions and repository aliases) ---

  function openRenameModal(kind, id, currentName) {
    state.renameTarget = { kind, id, currentName };
    renameTitle.textContent = kind === "repo" ? "Rename repository" : "Rename session";
    renameTitle.title = id || "";
    renameInput.value = currentName;
    renameError.classList.add("hidden");
    renameModal.classList.remove("hidden");
    renameInput.focus();
    renameInput.select();
  }

  function closeRenameModal() {
    renameModal.classList.add("hidden");
    state.renameTarget = null;
  }

  async function saveRename() {
    const target = state.renameTarget;
    if (!target) return;
    const name = renameInput.value;
    const path = target.kind === "repo"
      ? "/api/repos/alias"
      : "/api/sessions/" + encodeURIComponent(target.id) + "/name";
    const body = target.kind === "repo"
      ? JSON.stringify({ repository: target.id, alias: name })
      : JSON.stringify({ name });
    try {
      const resp = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
      });
      if (!resp.ok) {
        renameError.textContent = await resp.text();
        renameError.classList.remove("hidden");
        return;
      }
      closeRenameModal();
      refreshSessions();
    } catch (e) {
      renameError.textContent = String(e);
      renameError.classList.remove("hidden");
    }
  }

  document.getElementById("rename-cancel").addEventListener("click", closeRenameModal);
  document.getElementById("rename-save").addEventListener("click", saveRename);
  renameInput.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") saveRename();
    if (ev.key === "Escape") closeRenameModal();
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "F2" && !state.renameTarget) {
      ev.preventDefault();
      renameCursorSession();
    }
  });

  // --- Browser notifications on done/question transitions ---

  function requestNotificationPermission() {
    if (typeof Notification !== "undefined" && Notification.permission === "default") {
      Notification.requestPermission();
    }
  }

  function displayName(s) {
    return s.name || s.summary || (s.cwd ? s.cwd.split("/").pop() : s.id);
  }

  function notifyFor(kind, sessionId) {
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
    const s = state.sessions.find((x) => x.id === sessionId);
    const name = s ? displayName(s) : sessionId;
    const message = kind === "done" ? name + " done!" : name + " needs your input";
    new Notification(message);
  }

  function connectEvents() {
    const es = new EventSource("/api/events");
    es.addEventListener("notify", (ev) => {
      try {
        const payload = JSON.parse(ev.data);
        notifyFor(payload.kind, payload.sessionId);
      } catch (e) {
        // ignore malformed frames
      }
    });
    es.onerror = () => {
      // EventSource auto-reconnects; nothing else to do.
    };
  }

  function registerServiceWorker() {
    // Registering a service worker is what makes Chrome/Edge consider this
    // app installable; see sw.js's header comment for what it actually
    // does (cache-first for the static shell only, network passthrough for
    // everything else). Safari doesn't require this for "Add to Dock", so
    // this is skipped harmlessly there if unsupported.
    if ("serviceWorker" in navigator) {
      navigator.serviceWorker.register("/sw.js").catch(() => {
        // Installability is a nice-to-have, not a hard requirement — a
        // failed registration (e.g. served over a scheme that disallows
        // service workers) shouldn't block the rest of the app.
      });
    }
  }

  requestNotificationPermission();
  registerServiceWorker();
  restoreSidebarWidth();
  restoreCodeWidth();
  refreshSessions();
  setInterval(refreshSessions, REFRESH_INTERVAL_MS);
  connectEvents();
  updateSidebarState();
  updateFocusIndicator();
})();
