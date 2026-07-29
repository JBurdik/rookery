# Future plan (post-MVP, not scoped for v1)

Ideas gathered from Herdr's fuller feature set and from `stablyai/orca` (open-source parallel-coding-agent ADE, desktop+mobile+VPS, 29.5k★) as inspiration. Nothing here blocks MVP — revisit once the core daemon/attach/API loop is stable and used daily.

## From Orca

- **Mobile companion** — API socket is already plain JSON-RPC over Unix socket; a phone client needs a relay (SSH-forwarded socket, or a small HTTPS bridge) plus push notifications on pane status change (`agent done`, `agent blocked`). Natural extension of `pane.status`, no protocol redesign.
- **Parallel git worktrees** — fan one prompt across N agent panes, each in its own `git worktree`. Needs a `worktree.*` API namespace (`worktree.create`, `worktree.list`, `worktree.remove`), deliberately excluded from v1.
- **SSH-native remote worktrees with auto-reconnect** — design is already Unix-socket-first; formalize a documented SSH port/socket-forward recipe, then add a reconnect-on-drop loop in the attach client.
- **Notifications/unread state** — pane transitions (idle→done, idle→blocked) as OS notifications + an unread marker in the TUI tab bar.
- **Quick open** — fuzzy search across panes/sessions/commands in the attach client.
- **Account/usage tracking** — surface agent CLI rate-limit/usage info via `pane.status` if the wrapped agent CLI exposes it.
- **Annotate AI diffs / review flow** — probably a separate companion tool, not core daemon scope.
- **GUI client** — deferred on purpose; Orca proves "native-feeling wrapper around a terminal-heavy core" is a viable shape. Revisit toolkit choice (Wails/Tauri-style webview vs native SwiftUI) only once this comes up — the attach protocol is already structured-cell JSON so either approach can consume it without a redesign.

## From Herdr (deferred method surface)

- `workspace.*` / `tab.*` / `layout.*` — multi-workspace management, layout export/import, split-ratio persistence.
- Plugin system (`plugin.*`) for third-party pane types.
- Live-upgrade / handoff mechanism (Herdr's `handoff.rs`) — seamless daemon binary upgrade without dropping running PTYs, via FD passing to a new process.
- Kitty graphics protocol / image passthrough in panes.

## Open questions to revisit later

- Multi-client differing terminal sizes (v1: last attacher's viewport wins — needs a real per-client viewport story if this becomes annoying).
- Whether `internal/termgrid`'s `x/vt` dependency needs replacing once it stabilizes or if Bubble Tea v2 changes the calculus.

## Deferred out of the splits/status/wait round (2026-07-27)

- **Tabs and workspaces** (`w1:t1:p1`) — every pane is currently a split of one
  screen, with `ctrl+b z` (zoom) as the pressure valve when that gets crowded.
  This is the biggest remaining gap against Herdr.
- **Mouse** — click to focus, drag a divider to resize.
- **Layout persistence** across a daemon restart.
- **Per-client viewports** — the daemon renders one composite frame at the last
  attacher's size, so two clients with different terminal sizes share the
  smaller experience.
- **ANSI-aware truncation in the client** — frame lines are used at whatever
  width the daemon rendered them; a resize can flicker for exactly one frame.
- **Agent status rules as data** — `internal/agentstatus` holds them in a Go
  table. Herdr ships per-agent TOML manifests with an update channel; move to
  files if the rules start changing faster than the binary does.

## Deferred out of the Herdr-parity round (2026-07-27)

Shipped in that round: workspaces → tabs → panes, mouse (click/drag/scroll +
forwarding), agents-only sidebar panel with unread markers, `prefix ?` help,
and `~/.rook/{config,hotkeys}.json`.

Still missing against Herdr (copy mode, the scrollback viewport, layout
persistence and the goto picker have since landed — see the round below):

- **Swap panes** (`prefix+shift+hjkl`) and a resize *mode* rather than
  one-shot resize keys.
- **Per-client viewports** — one composite frame is rendered at the last
  attacher's size, so two clients with different terminal sizes share the
  smaller one.
- **Plugins and a mobile client.**

## Deferred out of the scroll/copy/goto/persistence round (2026-07-27)

Shipped in that round: a scroll viewport with copy mode (`prefix [`, wheel,
OSC 52 yank), layout persistence across a daemon restart, and the `prefix f`
goto picker.

Right-click context menus (2026-07-29): a pane header, a tab, or a sidebar
workspace row now opens a small popover at the click — rename/close/split/zoom
for a pane, rename/close/new for a tab, rename/new/close for a workspace.
Dismissed with esc or a click elsewhere; each item runs exactly the action its
own keybinding would (`prefix r` renames a pane, `prefix R` a workspace — both
new bindings, since neither existed before this).

Still open, in the order they are likely to bite:

- **Cell-accurate scrollback.** The viewport draws the plain-text transcript:
  no colour, no wrapping, and a full-screen redraw (an agent's TUI) is
  meaningless in it. A real one needs an emulator that keeps styled history —
  `charmbracelet/x/vt` has it, and is exactly the dependency `internal/termgrid`
  documents why it could not take.
- **Character selection and drag-to-select.** Selection is by whole lines. A
  cell-accurate history is a prerequisite for the first; the second also needs
  press/drag/release to be routed to the viewport rather than to focus.
- **Restoring what was running in a pane.** The commands are recorded in
  `layout.json` and deliberately not re-run. If it turns out to be wanted, the
  shape is a flag, not a redesign — but the default should stay "a shell".
- **Persisting scroll position / zoom across a restart.** Zoom is saved,
  scroll is not; the transcript it referred to is gone with the old process.
- **Picking across sessions.** The goto picker sees one session's state,
  because that is all a client is attached to.

## Agent manifests (2026-07-27)

Status rules now live in per-agent JSON manifests (`internal/agentstatus/
manifests/*.json`, embedded; overridable from `~/.rook/agents/`). Chosen over
Herdr's TOML because the rest of rookery's config is JSON and encoding/json is
stdlib.

Still missing versus Herdr's manifest system:
- **An update channel.** Herdr ships `manifest_update.rs` so rules can be
  refreshed without shipping a binary. Ours are only updated by a release or a
  hand-edited file.
- **`min_engine_version` / versioned manifests.** No compatibility gate, so a
  future rule field would be silently ignored by an older binary.
- **Richer regions.** Herdr has `after_last_horizontal_rule`, `whole_recent`,
  `osc_title`, `bottom_non_empty_lines(N)` with an argument; ours has `title`
  and `bottom` (fixed at 6 lines).
- **`skip_state_update` / `visible_blocker` flags**, which let a rule suppress
  a verdict rather than produce one (Herdr uses this for transcript viewers).
- **Per-agent integrations** — reading an agent's own session files rather than
  scraping its screen.
