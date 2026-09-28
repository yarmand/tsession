const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function bridge(native, popup) {
  const opened = [];
  const listeners = new Map();
  const frameWindow = {
    location: { href: "http://127.0.0.1:4270/api/code/abc/" },
    open: popup,
  };
  const frame = {
    contentWindow: frameWindow,
    contentDocument: {
      addEventListener(name, fn) { listeners.set(name, fn); },
    },
    addEventListener(name, fn) { listeners.set(name, fn); },
  };
  const root = {};
  const code = fs.readFileSync(path.join(__dirname, "../static/external.js"), "utf8");
  vm.runInNewContext(code, { window: root, URL });
  root.tsessionPopupBridge.install(frame, {
    native,
    openExternal(url) { opened.push(url); return Promise.resolve(); },
    onError(error) { throw error; },
  });
  listeners.get("load")();
  return { frameWindow, listeners, opened };
}

test("native code view opens sign-in URL in system browser instead of an unsupported popup", () => {
  const { frameWindow, opened } = bridge(true, () => {
    throw new Error("embedded WebKit cannot create a popup");
  });
  frameWindow.open("https://github.com/login/device", "_blank");
  assert.deepEqual(opened, ["https://github.com/login/device"]);
});

test("ordinary browser retains working popups, falling back only when blocked", () => {
  const popup = {};
  const working = bridge(false, () => popup);
  assert.equal(working.frameWindow.open("https://github.com/login/device"), popup);
  assert.deepEqual(working.opened, []);

  const blocked = bridge(false, () => null);
  blocked.frameWindow.open("https://github.com/login/device");
  assert.deepEqual(blocked.opened, ["https://github.com/login/device"]);
});

test("browser falls back when popup API throws", () => {
  const blocked = bridge(false, () => { throw new Error("popup blocked"); });
  assert.doesNotThrow(() => blocked.frameWindow.open("https://github.com/login/device"));
  assert.deepEqual(blocked.opened, ["https://github.com/login/device"]);
});

test("noopener popups stay detectable so a working browser popup is not duplicated", () => {
  const popup = { opener: {} };
  const browser = bridge(false, (url, target, features) => {
    assert.ok(!/noopener|noreferrer/i.test(features || ""), "features must stay detectable");
    return popup;
  });
  browser.frameWindow.open("https://github.com/login/device", "_blank", "noopener,noreferrer");
  assert.deepEqual(browser.opened, []);
  assert.equal(popup.opener, null, "noopener intent must still be honoured");
});

// VS Code reserves a blank popup while the user gesture is still active and
// only assigns the sign-in URL once the auth extension has produced it.
test("sign-in through VS Code's reserved blank popup reaches the system browser", () => {
  const { frameWindow, opened } = bridge(true, () => null);

  const reserved = frameWindow.open();
  assert.ok(reserved, "reservation must succeed for VS Code to continue");
  assert.equal(reserved.closed, false);

  reserved.document.title = "Signing in";
  reserved.document.documentElement.style.cssText = "color-scheme:light dark";
  reserved.document.body.textContent = "Signing in";

  reserved.opener = null;
  reserved.location.href = "https://github.com/login/device";

  assert.deepEqual(opened, ["https://github.com/login/device"]);
});

test("a reserved popup blocked by the browser still completes sign-in", () => {
  const { frameWindow, opened } = bridge(false, () => null);
  const reserved = frameWindow.open();
  assert.ok(reserved);
  reserved.location.href = "https://github.com/login/device";
  assert.deepEqual(opened, ["https://github.com/login/device"]);
});

test("reserved popups can be closed and report being closed", () => {
  const { frameWindow } = bridge(true, () => null);
  const reserved = frameWindow.open();
  reserved.close();
  assert.equal(reserved.closed, true);
});

test("non-http popup targets are left to the frame itself", () => {
  const calls = [];
  const { frameWindow } = bridge(true, (url) => { calls.push(url); return null; });
  frameWindow.open("blob:http://127.0.0.1:4270/1234", "_blank");
  assert.deepEqual(calls, ["blob:http://127.0.0.1:4270/1234"]);
});

test("native external target links open in browser without navigating VS Code iframe", () => {
  const { listeners, opened } = bridge(true, () => null);
  let prevented = false;
  listeners.get("click")({
    target: {
      closest() { return { href: "https://github.com/login/device", target: "_blank" }; },
    },
    preventDefault() { prevented = true; },
  });
  assert.equal(prevented, true);
  assert.deepEqual(opened, ["https://github.com/login/device"]);
});
