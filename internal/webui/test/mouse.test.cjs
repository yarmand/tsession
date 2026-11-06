const { test } = require("node:test");
const assert = require("node:assert/strict");
const { Terminal } = require("../static/vendor/xterm.js");

// Keep xterm's real parser, mouse service and input encoding; only replace
// the DOM and pixel-to-cell measurement, which need a browser layout engine.
function setup(t) {
  const term = new Terminal();
  const core = term._core;
  const element = new EventTarget();
  element.classList = { add() {}, remove() {} };
  const document = new EventTarget();
  core.element = element;
  core._document = document;
  core.screenElement = {};
  core._mouseService = {
    getMouseReportCoords: (ev) => ({ col: ev.col, row: ev.row, x: ev.col, y: ev.row }),
  };
  core._selectionService = {
    disable() {}, enable() {}, shouldForceSelection: () => false,
  };
  core.focus = () => {};
  core.cancel = () => {};
  core.bindMouse();
  t.after(() => term.dispose());

  const sent = [];
  term.onData((data) => sent.push(data));
  function mouse(target, type, col, row, buttons = 1) {
    const ev = new Event(type, { cancelable: true });
    Object.assign(ev, { col, row, buttons, button: 0 });
    target.dispatchEvent(ev);
  }
  const write = (data) => new Promise((resolve) => term.write(data, resolve));
  return { term, element, document, sent, mouse, write };
}

// tmux emits this reset/re-enable sequence when an application requesting
// all mouse motion (such as Copilot) is replaced by its own copy mode.
const COPY_MODE_MOUSE = "\x1b[?1006l\x1b[?1000l\x1b[?1002l\x1b[?1003l" +
  "\x1b[?1006h\x1b[?1000h\x1b[?1002h";

test("tmux copy-mode protocol switch preserves an in-progress mouse drag", async (t) => {
  const { element, document, sent, mouse, write } = setup(t);
  await write("\x1b[?1006h\x1b[?1003h");
  mouse(element, "mousedown", 9, 4);
  await write(COPY_MODE_MOUSE);
  mouse(document, "mousemove", 19, 6);
  mouse(document, "mouseup", 19, 6, 0);

  assert.deepEqual(sent, [
    "\x1b[<0;10;5M",
    "\x1b[<32;20;7M",
    "\x1b[<0;20;7m",
  ]);
});

test("release while mouse tracking is disabled ends the drag", async (t) => {
  const { element, document, sent, mouse, write } = setup(t);
  await write("\x1b[?1006h\x1b[?1003h");
  mouse(element, "mousedown", 9, 4);
  await write("\x1b[?1003l");
  mouse(document, "mouseup", 19, 6, 0);
  await write("\x1b[?1002h");
  // A new drag starting outside the terminal must not report to this pane.
  mouse(document, "mousemove", 29, 8);

  assert.deepEqual(sent, ["\x1b[<0;10;5M"]);
});

test("normal tmux drag reports once and stops on release", async (t) => {
  const { element, document, sent, mouse, write } = setup(t);
  await write("\x1b[?1006h\x1b[?1002h");
  mouse(element, "mousedown", 9, 4);
  mouse(document, "mousemove", 19, 6);
  mouse(document, "mouseup", 19, 6, 0);
  mouse(document, "mousemove", 29, 8);

  assert.deepEqual(sent, [
    "\x1b[<0;10;5M",
    "\x1b[<32;20;7M",
    "\x1b[<0;20;7m",
  ]);
});
