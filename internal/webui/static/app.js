// tsession web UI client. No build step, no framework: this file is served
// as-is via go:embed and runs directly in the browser.
(() => {
  "use strict";

  const REFRESH_INTERVAL_MS = 5000;

  const state = {
    sessions: [],
    selectedKey: null, // `${origin}\u0000${id}`
    socket: null,
    term: null,
    fitAddon: null,
    renameTarget: null, // { kind: "session"|"repo", id, currentName }
    focusTarget: "list", // "list" | "terminal" — see the Focus management section below
    listIndex: 0, // keyboard-navigation cursor row in state.sessions
  };

  const listEl = document.getElementById("session-list");
  const terminalEl = document.getElementById("terminal");
  const emptyStateEl = document.getElementById("empty-state");
  const bannerEl = document.getElementById("terminal-banner");
  const renameModal = document.getElementById("rename-modal");
  const renameInput = document.getElementById("rename-input");
  const renameTitle = document.getElementById("rename-modal-title");
  const renameError = document.getElementById("rename-error");

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

  function renderSessions() {
    listEl.innerHTML = "";
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

      const repo = document.createElement("span");
      repo.className = "repo";
      repo.textContent = s.repository || s.cwd || s.id;
      repo.title = "Double-click a row to rename the session; right-click the repository name to set an alias.";
      line1.appendChild(repo);

      const age = document.createElement("span");
      age.className = "age";
      age.textContent = formatAge(s.lastEventAt || s.updatedAt);
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
        openRenameModal("repo", s.repositoryId || s.repository, s.repository || "");
      });
      listEl.appendChild(li);
    });
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

  function ensureTerminal() {
    if (state.term) return;
    state.term = new Terminal({
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
    state.fitAddon = new FitAddon.FitAddon();
    state.term.loadAddon(state.fitAddon);
    state.term.open(terminalEl);
    state.term.onResize(() => sendResize());
    // Deferring the initial fit to the next frame ensures the container has
    // already been laid out (it was just unhidden by selectSession), so the
    // canvas backing store is sized against the real devicePixelRatio
    // instead of a stale/zero layout, which otherwise shows up as blurry
    // upscaled text.
    requestAnimationFrame(() => state.fitAddon.fit());

    const encoder = new TextEncoder();
    state.term.onData((data) => {
      if (state.socket && state.socket.readyState === WebSocket.OPEN) {
        // WebSocket.send(string) always sends a TEXT frame, but the server
        // only treats keystrokes as PTY input on BINARY frames (TEXT frames
        // are parsed as JSON control messages, e.g. resize) — so keystrokes
        // must be sent as bytes, not as a string.
        state.socket.send(encoder.encode(data));
      }
    });

    // xterm.js only routes keydown to the PTY when its own hidden textarea
    // has real DOM focus. attachCustomKeyEventHandler runs before xterm's
    // internal handling; stopping propagation here keeps page-level
    // shortcuts (rename modal Escape, etc.) from also reacting to keys the
    // user is sending to the terminal.
    state.term.attachCustomKeyEventHandler((ev) => {
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
          if (state.socket && state.socket.readyState === WebSocket.OPEN) {
            state.socket.send(encoder.encode(seq));
          }
          ev.stopPropagation();
          return false;
        }
      }
      ev.stopPropagation();
      return true; // let xterm handle it normally
    });

    const resizeObserver = new ResizeObserver(() => {
      if (state.fitAddon) state.fitAddon.fit();
    });
    resizeObserver.observe(terminalEl);
    window.addEventListener("resize", () => {
      if (state.fitAddon) state.fitAddon.fit();
    });
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

  function selectSession(s) {
    state.selectedSession = s;
    state.selectedKey = sessionKey(s);
    const idx = state.sessions.findIndex((x) => sessionKey(x) === state.selectedKey);
    if (idx >= 0) state.listIndex = idx;
    renderSessions();

    emptyStateEl.classList.add("hidden");
    terminalEl.classList.remove("hidden");
    bannerEl.classList.add("hidden");

    ensureTerminal();
    state.term.reset();

    if (state.socket) {
      state.socket.close();
      state.socket = null;
    }

    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    const url = wsScheme() + "//" + location.host + "/api/terminal/" + originSegment + "/" + encodeURIComponent(s.id);
    const socket = new WebSocket(url);
    socket.binaryType = "arraybuffer";
    state.socket = socket;

    socket.addEventListener("open", () => {
      sendResize();
      focusTerminal();
    });
    socket.addEventListener("message", (ev) => {
      if (typeof ev.data === "string") {
        state.term.write(ev.data);
      } else {
        state.term.write(new Uint8Array(ev.data));
      }
    });
    socket.addEventListener("close", (ev) => {
      if (state.socket === socket) {
        showBanner("Connection closed" + (ev.reason ? ": " + ev.reason : "") + ".");
      }
    });
    socket.addEventListener("error", () => {
      if (state.socket === socket) {
        showBanner("Failed to connect to session.");
      }
    });
  }

  function sendResize() {
    if (!state.socket || state.socket.readyState !== WebSocket.OPEN || !state.term) return;
    const msg = JSON.stringify({ type: "resize", cols: state.term.cols, rows: state.term.rows });
    state.socket.send(msg);
  }

  // --- Focus management: Cmd+/ (or Ctrl+/) toggles keyboard focus between
  // the session list and the terminal. While the terminal has focus, all
  // keystrokes go to the PTY; while the list has focus, arrow keys move a
  // highlight and Enter attaches. Browser-overridable shortcuts (Cmd+F,
  // Cmd+S, etc.) are suppressed while the terminal is focused so they reach
  // tmux instead — a few shortcuts (Cmd+W/T/N/Q and similar) are reserved by
  // the browser/OS itself and cannot be intercepted from JavaScript.

  function focusTerminal() {
    if (!state.term || !state.selectedSession) return;
    state.focusTarget = "terminal";
    state.term.focus();
    updateFocusIndicator();
  }

  function focusList() {
    state.focusTarget = "list";
    if (state.term) state.term.blur();
    if (state.listIndex == null) state.listIndex = 0;
    updateFocusIndicator();
    renderSessions();
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

  const SUPPRESSABLE_KEYS = new Set([
    "f", "s", "p", "g", "k", "l", "o", "d", "u", "j", "e", "h", "n", "1", "2", "3", "4", "5", "6", "7", "8", "9",
  ]);

  document.addEventListener(
    "keydown",
    (ev) => {
      const key = ev.key.toLowerCase();
      const mod = ev.metaKey || ev.ctrlKey;

      // Cmd+/ (or Ctrl+/) toggles focus regardless of which panel is
      // currently focused.
      if (mod && key === "/") {
        ev.preventDefault();
        ev.stopPropagation();
        if (state.focusTarget === "terminal") focusList();
        else focusTerminal();
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
  document.getElementById("sessions").addEventListener("click", () => {
    if (state.focusTarget !== "list") focusList();
  });

  // --- Rename modal (sessions and repository aliases) ---

  function openRenameModal(kind, id, currentName) {
    state.renameTarget = { kind, id, currentName };
    renameTitle.textContent = kind === "repo" ? "Rename repository" : "Rename session";
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
    if (ev.key === "F2" && state.selectedSession) {
      openRenameModal("session", state.selectedSession.id, state.selectedSession.name || "");
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
  refreshSessions();
  setInterval(refreshSessions, REFRESH_INTERVAL_MS);
  connectEvents();
  updateFocusIndicator();
})();
