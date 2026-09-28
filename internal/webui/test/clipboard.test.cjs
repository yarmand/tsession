const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function load() {
  const root = {};
  const code = fs.readFileSync(path.join(__dirname, "../static/clipboard.js"), "utf8");
  vm.runInNewContext(code, { window: root, TextDecoder, atob, Promise });
  return root.tsessionClipboard;
}

function b64(s) {
  return Buffer.from(s, "utf8").toString("base64");
}

// A stand-in for xterm.js's parser, which is where OSC sequences surface.
function fakeTerm() {
  const handlers = new Map();
  return {
    parser: {
      registerOscHandler(ident, cb) {
        handlers.set(ident, cb);
        return { dispose() { handlers.delete(ident); } };
      },
    },
    emitOsc(ident, payload) {
      const cb = handlers.get(ident);
      if (!cb) throw new Error("no handler registered for OSC " + ident);
      return cb(payload);
    },
    hasHandler(ident) { return handlers.has(ident); },
  };
}

function installed(opts = {}) {
  const clip = load();
  const term = fakeTerm();
  const written = [];
  const errors = [];
  clip.install(term, {
    writeText: opts.writeText || ((t) => { written.push(t); return Promise.resolve(); }),
    onError: (e) => errors.push(String(e)),
  });
  return { clip, term, written, errors };
}

test("tmux copy arrives as OSC 52 and reaches the system clipboard", () => {
  const { term, written } = installed();
  const handled = term.emitOsc(52, "c;" + b64("copied from remote tmux"));
  assert.equal(handled, true, "handler must consume the sequence");
  assert.deepEqual(written, ["copied from remote tmux"]);
});

test("non-ASCII selections survive the base64 round trip", () => {
  const { term, written } = installed();
  term.emitOsc(52, "c;" + b64("héllo → wörld ✓"));
  assert.deepEqual(written, ["héllo → wörld ✓"]);
});

test("any OSC 52 target is honoured, not just the clipboard selection", () => {
  const { term, written } = installed();
  term.emitOsc(52, "p;" + b64("primary"));
  term.emitOsc(52, ";" + b64("default"));
  assert.deepEqual(written, ["primary", "default"]);
});

// A remote host must never be able to read what is on the local clipboard.
test("a clipboard read query is refused rather than answered", () => {
  const { term, written, errors } = installed();
  const handled = term.emitOsc(52, "c;?");
  assert.equal(handled, true, "the query must still be swallowed, not rendered");
  assert.deepEqual(written, []);
  assert.deepEqual(errors, []);
});

test("an empty payload does not wipe the existing clipboard", () => {
  const { term, written } = installed();
  term.emitOsc(52, "c;");
  assert.deepEqual(written, []);
});

test("malformed sequences are reported without throwing or rendering", () => {
  const { term, written, errors } = installed();
  let handled;
  assert.doesNotThrow(() => { handled = term.emitOsc(52, "c;!!!not base64!!!"); });
  assert.equal(handled, true);
  assert.deepEqual(written, []);
  assert.equal(errors.length, 1);
});

test("a rejected clipboard write surfaces as an error, not an unhandled rejection", async () => {
  const { term, errors } = installed({
    writeText: () => Promise.reject(new Error("denied")),
  });
  term.emitOsc(52, "c;" + b64("text"));
  await new Promise((r) => setTimeout(r, 10));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /denied/);
});

test("systemWriter prefers the async clipboard API", async () => {
  const clip = load();
  const calls = [];
  const write = clip.systemWriter(
    { clipboard: { writeText: (t) => { calls.push(t); return Promise.resolve(); } } },
    null,
  );
  await write("hello");
  assert.deepEqual(calls, ["hello"]);
});

// navigator.clipboard.writeText rejects without transient user activation,
// which a terminal escape sequence never has.
test("systemWriter falls back to a copy command when the clipboard API is denied", async () => {
  const clip = load();
  const execs = [];
  const el = { value: "", style: {}, focus() {}, select() {}, setAttribute() {} };
  const doc = {
    createElement: () => el,
    body: { appendChild() {}, removeChild() {} },
    execCommand: (cmd) => { execs.push([cmd, el.value]); return true; },
  };
  const write = clip.systemWriter(
    { clipboard: { writeText: () => Promise.reject(new Error("NotAllowed")) } },
    doc,
  );
  await write("fallback text");
  assert.deepEqual(execs, [["copy", "fallback text"]]);
});

test("systemWriter reports failure when no mechanism works", async () => {
  const clip = load();
  const doc = {
    createElement: () => ({ value: "", style: {}, focus() {}, select() {}, setAttribute() {} }),
    body: { appendChild() {}, removeChild() {} },
    execCommand: () => false,
  };
  const write = clip.systemWriter({}, doc);
  await assert.rejects(() => write("nope"));
});
