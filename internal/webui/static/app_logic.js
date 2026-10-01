(function (root, factory) {
  const logic = factory();
  if (typeof module === "object" && module.exports) module.exports = logic;
  root.tsessionAppLogic = logic;
})(typeof globalThis !== "undefined" ? globalThis : this, () => {
  "use strict";

  const LOCAL_TERMINAL = Object.freeze({
    id: "__local-terminal__",
    origin: "",
    localTerminal: true,
    name: "Local terminal",
    summary: "Shell in your home directory",
  });

  function sessionKey(s) {
    return (s.origin || "") + "\u0000" + s.id;
  }

  function listRows(sessions) {
    return sessions.concat([LOCAL_TERMINAL]);
  }

  function clampListIndex(index, sessions) {
    const rowCount = listRows(sessions).length;
    if (rowCount === 0) return 0;
    if (index == null) return 0;
    return Math.min(rowCount - 1, Math.max(0, index));
  }

  function rowIndexForKey(sessions, selectedKey) {
    return listRows(sessions).findIndex((s) => sessionKey(s) === selectedKey);
  }

  function isPlainAltChord(ev, code) {
    return !!ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === code;
  }

  function captureKeyAction(ev, focusTarget) {
    if (isPlainAltChord(ev, "KeyH")) return "toggle-sidebar";
    if (isPlainAltChord(ev, "Slash")) {
      return focusTarget === "terminal" ? "focus-list" : "focus-terminal";
    }
    if (isPlainAltChord(ev, "KeyT")) return "open-local-terminal";
    if (isPlainAltChord(ev, "KeyZ")) return "toggle-pane-zoom";
    if (isPlainAltChord(ev, "KeyE") && focusTarget === "list") {
      return "toggle-code-view";
    }
    return "";
  }

  function terminalSocketPath(s) {
    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    return s.localTerminal
      ? "/api/localterm"
      : "/api/terminal/" + originSegment + "/" + encodeURIComponent(s.id);
  }

  return {
    LOCAL_TERMINAL,
    sessionKey,
    listRows,
    clampListIndex,
    rowIndexForKey,
    captureKeyAction,
    terminalSocketPath,
  };
});
