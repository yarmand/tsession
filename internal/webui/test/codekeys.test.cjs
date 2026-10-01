const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function load() {
  const root = {};
  const code = fs.readFileSync(path.join(__dirname, "../static/codekeys.js"), "utf8");
  vm.runInNewContext(code, { window: root });
  return root.tsessionCodeKeys;
}

// Minimal event target recording listeners with their capture flag.
function target() {
  const listeners = [];
  return {
    listeners,
    addEventListener(type, cb, capture) { listeners.push({ type, cb, capture: !!capture }); },
    fire(type, ev) { for (const l of listeners) if (l.type === type) l.cb(ev); },
  };
}

function keyEvent(fields) {
  const ev = {
    altKey: false, ctrlKey: false, metaKey: false, shiftKey: false, code: "", key: "",
    prevented: false, stopped: false,
    preventDefault() { this.prevented = true; },
    stopPropagation() { this.stopped = true; },
    stopImmediatePropagation() { this.stopped = true; },
    ...fields,
  };
  return ev;
}

function setup() {
  const keys = load();
  const frame = target();
  const win = target();
  frame.contentWindow = win;
  let calls = 0;
  let zoomCalls = 0;
  let activateCalls = 0;
  const errors = [];
  keys.install(frame, {
    onFocusTerminal: () => { calls++; },
    onToggleZoom: () => { zoomCalls++; },
    onActivate: () => { activateCalls++; },
    onError: (e) => errors.push(e),
  });
  frame.fire("load", {});
  return {
    win,
    calls: () => calls,
    zoomCalls: () => zoomCalls,
    activateCalls: () => activateCalls,
    errors,
  };
}

test("Alt+/ inside the VS Code frame moves focus to the terminal", () => {
  const { win, calls } = setup();
  const ev = keyEvent({ altKey: true, code: "Slash", key: "÷" });
  win.fire("keydown", ev);
  assert.equal(calls(), 1);
  assert.ok(ev.prevented, "VS Code must not also receive the chord");
  assert.ok(ev.stopped);
});

test("listener runs in the capture phase so VS Code cannot swallow it first", () => {
  const { win } = setup();
  const l = win.listeners.find((x) => x.type === "keydown");
  assert.ok(l, "no keydown listener installed on the frame window");
  assert.equal(l.capture, true);
});

test("Alt+Z inside the VS Code frame zooms the code pane", () => {
  const { win, zoomCalls, activateCalls } = setup();
  const ev = keyEvent({ altKey: true, code: "KeyZ", key: "Ω" });
  win.fire("keydown", ev);
  assert.equal(zoomCalls(), 1);
  assert.equal(activateCalls(), 1);
  assert.ok(ev.prevented, "VS Code must not also receive the chord");
  assert.ok(ev.stopped);
});

test("focus and pointer activity mark the code pane active", () => {
  const { win, activateCalls } = setup();
  win.fire("focus", {});
  win.fire("pointerdown", {});
  assert.equal(activateCalls(), 2);
});

test("other keys and modified Slash chords pass through to VS Code untouched", () => {
  const { win, calls } = setup();
  for (const f of [
    { code: "Slash", key: "/" },
    { altKey: true, shiftKey: true, code: "Slash" },
    { altKey: true, metaKey: true, code: "Slash" },
    { altKey: true, ctrlKey: true, code: "Slash" },
    { altKey: true, code: "KeyE" },
    { altKey: true, shiftKey: true, code: "KeyZ" },
  ]) {
    const ev = keyEvent(f);
    win.fire("keydown", ev);
    assert.equal(ev.prevented, false, JSON.stringify(f));
  }
  assert.equal(calls(), 0);
});

test("re-installs after the frame navigates (each load gets a fresh window)", () => {
  const keys = load();
  const frame = target();
  let calls = 0;
  keys.install(frame, { onFocusTerminal: () => { calls++; } });
  const first = target();
  frame.contentWindow = first;
  frame.fire("load", {});
  const second = target();
  frame.contentWindow = second;
  frame.fire("load", {});
  second.fire("keydown", keyEvent({ altKey: true, code: "Slash" }));
  assert.equal(calls, 1);
});

test("a cross-origin frame reports an error instead of throwing", () => {
  const keys = load();
  const frame = target();
  Object.defineProperty(frame, "contentWindow", { get() { throw new Error("SecurityError"); } });
  const errors = [];
  keys.install(frame, { onFocusTerminal() {}, onError: (e) => errors.push(e) });
  assert.doesNotThrow(() => frame.fire("load", {}));
  assert.equal(errors.length, 1);
});
