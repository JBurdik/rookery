// installed by rook
// managed by rook; reinstalling the integration overwrites this file.
// add custom plugins beside this file instead of editing it.
// rookery-integration

import { execFile } from "node:child_process";

const ROOK_BIN = "__ROOK_BIN__";
const AGENT = "opencode";

function report(args) {
  execFile(ROOK_BIN, ["report", "--agent", AGENT, "--quiet", ...args], () => {});
}

// A state report is authoritative and replaces whatever screen detection
// currently shows for the pane.
function reportState(state, sessionID) {
  const args = ["--status", state];
  if (sessionID) args.push("--session-ref", sessionID);
  report(args);
}

// A session report on its own must not touch lifecycle status — omitting
// --status here (rather than passing an empty one) is what keeps rook from
// clearing the pane back to screen detection on every session.updated.
function reportSession(sessionID) {
  if (!sessionID) return;
  report(["--session-ref", sessionID]);
}

function stateFromSessionStatus(status) {
  const kind = typeof status === "string" ? status : status?.type;
  if (typeof kind !== "string") return undefined;
  switch (kind.toLowerCase()) {
    case "idle":
      return "idle";
    case "active":
    case "busy":
    case "pending":
    case "running":
    case "streaming":
    case "working":
    case "retry":
      return "working";
    default:
      return undefined;
  }
}

export const RookAgentStatePlugin = async () => {
  if (process.env.ROOK_ENV !== "1" || !process.env.ROOK_PANE) {
    return {};
  }

  return {
    "chat.message": async ({ sessionID }) => {
      reportState("working", sessionID);
    },
    event: async ({ event }) => {
      const type = event?.type;
      const properties = event?.properties ?? {};
      const sessionID =
        typeof properties?.sessionID === "string" ? properties.sessionID : undefined;

      switch (type) {
        case "session.created":
        case "session.updated":
          reportSession(sessionID);
          break;
        case "session.status": {
          const state = stateFromSessionStatus(properties.status);
          if (state) reportState(state, sessionID);
          break;
        }
        case "tool.execute.before":
        case "tool.execute.after":
        case "permission.replied":
        case "question.replied":
        case "question.rejected":
        case "session.compacted":
          reportState("working", sessionID);
          break;
        case "permission.asked":
        case "question.asked":
        case "session.error":
          reportState("blocked", sessionID);
          break;
        case "session.idle":
          reportState("idle", sessionID);
          break;
        default:
          break;
      }
    },
  };
};
