// tmux copies through OSC 52: when its selection is made it emits
// ESC ] 52 ; <targets> ; <base64> BEL to whatever terminal it is attached to.
// A native terminal (iTerm, Terminal.app) turns that into a system clipboard
// write. xterm.js registers handlers for OSC 0/1/2/4/8/10-12/104/110-112 but
// not 52, so the sequence was silently dropped and a copy made inside the web
// terminal never left the session's host — most visibly for a remote session,
// whose tmux has no other route to the local clipboard.
window.tsessionClipboard = {
  // decode turns an OSC 52 payload ("<targets>;<base64>") into its text.
  // It returns null when there is nothing to copy: an empty payload (which
  // must not clobber the clipboard) or a "?" read query, which is a request
  // for the clipboard's *contents* and is deliberately never answered — the
  // session's host, possibly remote, has no business reading what the user
  // has copied locally.
  decode(payload) {
    const raw = String(payload == null ? "" : payload);
    const semi = raw.indexOf(";");
    const data = semi === -1 ? raw : raw.slice(semi + 1);
    if (data === "" || data === "?") return null;

    const binary = atob(data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  },

  // systemWriter builds the text -> clipboard function used in the browser.
  // navigator.clipboard.writeText is preferred, but it requires transient
  // user activation, which an escape sequence arriving over a WebSocket never
  // has; browsers reject it with NotAllowedError. The execCommand("copy")
  // path still works from a non-gesture context, so it backs the modern API
  // rather than replacing it.
  systemWriter(nav, doc) {
    const fallback = (text) => {
      if (!doc || typeof doc.execCommand !== "function") {
        throw new Error("no clipboard mechanism available");
      }
      const area = doc.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      // Keep it out of sight and prevent the page from scrolling to it.
      area.style.position = "fixed";
      area.style.top = "-1000px";
      area.style.opacity = "0";
      doc.body.appendChild(area);
      try {
        area.focus();
        area.select();
        if (!doc.execCommand("copy")) {
          throw new Error("copy command was rejected");
        }
      } finally {
        doc.body.removeChild(area);
      }
    };

    return (text) => {
      if (nav && nav.clipboard && typeof nav.clipboard.writeText === "function") {
        return Promise.resolve(nav.clipboard.writeText(text)).catch(() => fallback(text));
      }
      return new Promise((resolve) => resolve(fallback(text)));
    };
  },

  // install registers the OSC 52 handler on an xterm.js terminal. The handler
  // always reports the sequence as handled so a malformed or refused one is
  // swallowed rather than printed into the session as stray text.
  install(term, { writeText, onError }) {
    const report = (error) => {
      if (typeof onError === "function") onError(error);
    };

    return term.parser.registerOscHandler(52, (payload) => {
      try {
        const text = this.decode(payload);
        if (text === null) return true;
        Promise.resolve(writeText(text)).catch(report);
      } catch (error) {
        report(error);
      }
      return true;
    });
  },
};
