// installed by rook
// managed by rook; reinstalling the integration overwrites this file.
// add custom extensions beside this file instead of editing it.
// rookery-pi-integration

import { execFile } from "node:child_process";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

const ROOK_BIN = "__ROOK_BIN__";
const AGENT = "pi";

function report(args: string[]) {
  execFile(ROOK_BIN, ["report", "--agent", AGENT, "--quiet", ...args], () => {});
}

function sessionRef(ctx: ExtensionContext): string | undefined {
  // Pi exposes both a stable id and the JSONL path. Prefer the id, but retain
  // the path as a useful resume reference on versions that have not made an
  // id yet (for example, before the first session entry is saved).
  return ctx.sessionManager.getSessionId() ?? ctx.sessionManager.getSessionFile();
}

function reportState(state: "idle" | "working", ctx: ExtensionContext) {
  const args = ["--status", state];
  const ref = sessionRef(ctx);
  if (ref) args.push("--session-ref", ref);
  report(args);
}

export default function rookAgentState(pi: ExtensionAPI) {
  // Pi loads global extensions even outside Rook. Be a complete no-op in that
  // case so installing this does not add a dependency or a visible side effect
  // to ordinary Pi sessions.
  if (process.env.ROOK_ENV !== "1" || !process.env.ROOK_PANE) return;

  pi.on("session_start", (_event, ctx) => {
    const ref = sessionRef(ctx);
    if (ref) report(["--session-ref", ref]);
    reportState("idle", ctx);
  });
  pi.on("agent_start", (_event, ctx) => reportState("working", ctx));
  // agent_settled is deliberately used instead of agent_end: it runs only
  // once there is no retry, compaction, or queued continuation left to do.
  pi.on("agent_settled", (_event, ctx) => reportState("idle", ctx));
}
