// Keyboard shortcuts that must work while focus is inside the VS Code code
// pane. Keydown events inside the (same-origin) iframe never bubble to the
// parent document, so app.js's global handler cannot see them; this hooks the
// frame's own window instead, in the capture phase so VS Code cannot consume
// the chord first.
window.tsessionCodeKeys = {
  install(frame, { onFocusTerminal, onToggleZoom = () => {}, onActivate = () => {}, onError = () => {} }) {
    frame.addEventListener("load", () => {
      try {
        const win = frame.contentWindow;
        if (!win) throw new Error("VS Code frame has no window");
        win.addEventListener("focus", onActivate, true);
        win.addEventListener("pointerdown", onActivate, true);
        win.addEventListener("keydown", (ev) => {
          // Alt+/ — match the physical key, as macOS Option composes ev.key.
          if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "Slash") {
            ev.preventDefault();
            ev.stopImmediatePropagation();
            onFocusTerminal();
          } else if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "KeyZ") {
            ev.preventDefault();
            ev.stopImmediatePropagation();
            onActivate();
            onToggleZoom();
          }
        }, true);
      } catch (error) {
        onError(error);
      }
    });
  },
};
