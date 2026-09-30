const test = require("node:test");
const assert = require("node:assert/strict");

const {
  LOCAL_TERMINAL,
  clampListIndex,
  listRows,
  rowIndexForKey,
  captureKeyAction,
  sessionKey,
  terminalSocketPath,
} = require("./app_logic.js");

test("listRows appends the pinned local terminal after real sessions", () => {
  const sessions = [{ id: "s1", origin: "" }, { id: "s2", origin: "remote" }];
  const rows = listRows(sessions);

  assert.equal(rows.length, 3);
  assert.equal(rows[0].id, "s1");
  assert.equal(rows[1].id, "s2");
  assert.equal(rows[2].id, LOCAL_TERMINAL.id);
  assert.equal(rows[2].localTerminal, true);
  assert.equal(sessions.length, 2);
});

test("clampListIndex uses the combined row model so the pinned row remains reachable", () => {
  const sessions = [{ id: "s1", origin: "" }, { id: "s2", origin: "" }];

  assert.equal(clampListIndex(null, sessions), 0);
  assert.equal(clampListIndex(-5, sessions), 0);
  assert.equal(clampListIndex(1, sessions), 1);
  assert.equal(clampListIndex(99, sessions), 2);
});

test("rowIndexForKey selects both agent rows and the pinned local terminal row", () => {
  const sessions = [{ id: "s1", origin: "" }, { id: "s2", origin: "remote-a" }];

  assert.equal(rowIndexForKey(sessions, sessionKey(sessions[1])), 1);
  assert.equal(rowIndexForKey(sessions, sessionKey(LOCAL_TERMINAL)), 2);
  assert.equal(rowIndexForKey(sessions, "missing"), -1);
});

test("captureKeyAction prioritizes Alt+T from either panel before terminal typing flow", () => {
  const altT = {
    altKey: true,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    code: "KeyT",
  };

  assert.equal(captureKeyAction(altT, "terminal"), "open-local-terminal");
  assert.equal(captureKeyAction(altT, "list"), "open-local-terminal");
  assert.equal(
    captureKeyAction({ ...altT, ctrlKey: true }, "terminal"),
    ""
  );
});

test("terminalSocketPath reserves /api/localterm for the pseudo-row only", () => {
  assert.equal(terminalSocketPath(LOCAL_TERMINAL), "/api/localterm");
  assert.equal(
    terminalSocketPath({ id: "id/1", origin: "ssh target", localTerminal: false }),
    "/api/terminal/ssh%20target/id%2F1"
  );
  assert.equal(
    terminalSocketPath({ id: "plain", origin: "", localTerminal: false }),
    "/api/terminal/local/plain"
  );
});
