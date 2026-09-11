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
    for (const s of state.sessions) {
      const key = sessionKey(s);
      const li = document.createElement("li");
      li.className = "session-row" + (key === state.selectedKey ? " selected" : "");
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

      li.addEventListener("click", () => selectSession(s));
      li.addEventListener("dblclick", () => openRenameModal("session", s.id, s.name || ""));
      repo.addEventListener("contextmenu", (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        openRenameModal("repo", s.repositoryId || s.repository, s.repository || "");
      });
      listEl.appendChild(li);
    }
  }

  async function refreshSessions() {
    try {
      const resp = await fetch("/api/sessions");
      if (!resp.ok) return;
      const data = await resp.json();
      state.sessions = data.sessions || [];
      renderSessions();
    } catch (e) {
      // Transient fetch failures are expected during server restarts; the
      // next poll retries.
    }
  }

  function wsScheme() {
    return location.protocol === "https:" ? "wss:" : "ws:";
  }

  function ensureTerminal() {
    if (state.term) return;
    state.term = new Terminal({
      convertEol: true,
      cursorBlink: true,
      fontSize: 13,
      theme: { background: "#000000" },
    });
    state.fitAddon = new FitAddon.FitAddon();
    state.term.loadAddon(state.fitAddon);
    state.term.open(terminalEl);
    state.fitAddon.fit();
    state.term.onData((data) => {
      if (state.socket && state.socket.readyState === WebSocket.OPEN) {
        state.socket.send(data);
      }
    });
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

  requestNotificationPermission();
  refreshSessions();
  setInterval(refreshSessions, REFRESH_INTERVAL_MS);
  connectEvents();
})();
