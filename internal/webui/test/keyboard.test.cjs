const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { Terminal } = require("../static/vendor/xterm.js");

const app = fs.readFileSync(path.join(__dirname, "../static/app.js"), "utf8");
const handler = app.match(/    term\.attachCustomKeyEventHandler\([\s\S]*?\n    \}\);/)[0];

// Exercise the application's handler with real xterm encoding, replacing
// only the browser DOM and WebSocket transport.
function setup(t) {
  const term = new Terminal();
  t.after(() => term.dispose());
  const sent = [];
  const encoder = new TextEncoder();
  const pane = { socket: { readyState: 1, send: (data) => sent.push(Array.from(data)) } };
  term.onData((data) => pane.socket.send(encoder.encode(data)));
  vm.runInNewContext(handler, {
    term, pane, encoder, WebSocket: { OPEN: 1 },
    // None of these test chords is an Alt+printable-character shortcut.
    altEscapeSequence: () => null,
  });

  const core = term._core;
  core._compositionHelper = { keydown: () => true };
  core.textarea = { value: "" };
  core.updateCursorStyle = () => {};
  core._showCursor = () => {};
  core.cancel = (ev) => ev.preventDefault();

  function key(overrides = {}) {
    const ev = {
      type: "keydown", key: "Enter", code: "Enter", keyCode: 13,
      shiftKey: false, ctrlKey: false, altKey: false, metaKey: false,
      isComposing: false, defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
      stopPropagation() {},
      ...overrides,
    };
    core._keyDown(ev);
    return ev;
  }
  return { term, sent, key };
}

test("Shift+Enter sends one newline byte, not the Enter submission byte", (t) => {
  const { term, sent, key } = setup(t);
  const ev = key({ shiftKey: true });
  assert.deepEqual(sent, [[10]]);
  assert.equal(ev.defaultPrevented, true);
  term._core._customKeyEventHandler({ ...ev, type: "keypress" });
  term._core._customKeyEventHandler({ ...ev, type: "keyup" });
  assert.deepEqual(sent, [[10]]);
});

test("Enter still submits and Ctrl+J still inserts a newline", (t) => {
  const { sent, key } = setup(t);
  key();
  key({ key: "j", code: "KeyJ", keyCode: 74, ctrlKey: true });
  assert.deepEqual(sent, [[13], [10]]);
});

test("Shift+Enter does not override additional modifiers or IME composition", (t) => {
  const { term, sent, key } = setup(t);
  for (const modifier of ["ctrlKey", "altKey", "metaKey", "isComposing"]) {
    const ev = key({ shiftKey: true, [modifier]: true });
    assert.notEqual(sent.at(-1)?.[0], 10, modifier);
    assert.equal(term._core._customKeyEventHandler({ ...ev, type: "keyup" }), true);
    assert.equal(term._core._customKeyEventHandler({ ...ev, type: "keypress" }), true);
  }
});
