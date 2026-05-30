import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import {
  writeFileSync,
  mkdirSync,
  readdirSync,
  unlinkSync,
  statSync,
  renameSync,
  readFileSync,
} from "node:fs";
import { join, basename } from "node:path";
import { homedir } from "node:os";

const STATE_DIR = join(homedir(), ".tsession", "pi-state");

interface PiState {
  id: string;
  state: "working" | "question" | "done" | "idle" | "exited";
  cwd: string;
  summary: string;
  updatedAt: string;
  pid: number;
  sessionFile: string;
}

export default function (pi: ExtensionAPI) {
  let sessionId: string | undefined;
  let cwd: string | undefined;
  let sessionFile: string | undefined;

  function writeState(state: PiState["state"], ctx?: ExtensionContext) {
    if (!sessionId) return;
    mkdirSync(STATE_DIR, { recursive: true });

    let summary = "";
    if (ctx) {
      summary = pi.getSessionName() ?? "";
      if (!summary) {
        for (const entry of ctx.sessionManager.getEntries()) {
          if (entry.type === "message" && entry.message?.role === "user") {
            const content = entry.message.content;
            if (typeof content === "string") {
              summary = content.slice(0, 100);
            } else if (Array.isArray(content)) {
              const text = content.find((c: any) => c.type === "text") as
                | { type: "text"; text: string }
                | undefined;
              if (text) summary = text.text.slice(0, 100);
            }
            break;
          }
        }
      }
    }

    const data: PiState = {
      id: sessionId,
      state,
      cwd: cwd ?? "",
      summary,
      updatedAt: new Date().toISOString(),
      pid: process.pid,
      sessionFile: sessionFile ?? "",
    };

    const filePath = join(STATE_DIR, `${sessionId}.json`);
    const tmpPath = join(STATE_DIR, `.${sessionId}.tmp`);
    writeFileSync(tmpPath, JSON.stringify(data, null, 2));
    renameSync(tmpPath, filePath);
  }

  function getLastAssistantText(ctx: ExtensionContext): string {
    const entries = ctx.sessionManager.getEntries();
    for (let i = entries.length - 1; i >= 0; i--) {
      const entry = entries[i];
      if (entry.type === "message" && entry.message?.role === "assistant") {
        const content = entry.message.content;
        if (Array.isArray(content)) {
          for (let j = content.length - 1; j >= 0; j--) {
            const block = content[j] as any;
            if (block.type === "text" && block.text) {
              return block.text.trimEnd();
            }
          }
        }
        break;
      }
    }
    return "";
  }

  function cleanupStaleFiles() {
    try {
      const files = readdirSync(STATE_DIR);
      const now = Date.now();
      for (const file of files) {
        if (!file.endsWith(".json") || file.startsWith(".")) continue;
        const filePath = join(STATE_DIR, file);
        try {
          const stat = statSync(filePath);
          if (now - stat.mtimeMs > 3600_000) {
            const raw = JSON.parse(readFileSync(filePath, "utf-8"));
            if (raw.state === "exited") {
              unlinkSync(filePath);
            }
          }
        } catch {
          // Skip files we can't read
        }
      }
    } catch {
      // Directory might not exist yet
    }
  }

  pi.on("session_start", async (_event, ctx) => {
    cwd = ctx.cwd;
    const sf = ctx.sessionManager.getSessionFile?.();
    sessionFile = sf ?? "";
    if (sf) {
      const base = basename(sf, ".jsonl");
      const underscoreIdx = base.indexOf("_");
      sessionId = underscoreIdx >= 0 ? base.slice(underscoreIdx + 1) : base;
    }
    writeState("idle", ctx);
    cleanupStaleFiles();
  });

  pi.on("turn_start", async (_event, ctx) => {
    writeState("working", ctx);
  });

  pi.on("agent_end", async (_event, ctx) => {
    const lastText = getLastAssistantText(ctx);
    const state = lastText.endsWith("?") ? "question" : "done";
    writeState(state, ctx);
  });

  pi.on("session_shutdown", async (_event, ctx) => {
    writeState("exited", ctx);
  });
}
