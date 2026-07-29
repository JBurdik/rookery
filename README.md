# rookery

A terminal multiplexer for coding agents. A detachable daemon keeps PTYs
alive, a Bubble Tea client attaches and detaches at will, and agents drive the
whole thing over a JSON-RPC Unix socket — spawning each other, watching each
other's status, and blocking until each other's work is done.

```
go build -o rook ./cmd/rook
./rook                # attach to the default session (starts the daemon if needed)
```

## Installing it

```
just install          # rook on your PATH, daemon restarted
just setup            # that, plus the Claude hooks and the agent skill
```

Which is `go install ./cmd/rook` — `$(go env GOPATH)/bin` is on your PATH after
a normal Go install, so there is no copy step and no `sudo`. The justfile is
there for the part that is easy to forget: a new binary changes nothing until
the running daemon goes away, so `install` kills it. `just` lists the rest
(`build`, `test`, `check`, `kill`, `uninstall`).

**After rebuilding, restart the daemon**: `rook` attaches to whatever daemon is
already running, so a new binary changes nothing until the old one goes away.

```
rook kill && ./rook
```

The client says so itself when the two versions differ, rather than leaving it
looking like the new behaviour is missing.

## The model

Same shape as Herdr: a **session** holds **workspaces**, a workspace holds
**tabs**, a tab holds a layout of **panes**. One workspace per repo or task,
tabs to separate views inside it (`agents`, `logs`, `review`), panes split
right and down. Ids say where something lives: `w1`, `w1:t2`, `w1:p3`.

```
 spaces              │ 1  2  3
 ● multiplexer       │1 ▶ Refactor the parser       │2 ! tests
    main             │✳ Cogitating… (12s · esc to   │Run failing test again? (y/n)
   review            │                              ├──────────────────────────────
                     │                              │3 ● shell
 agents              │                              │$
  multiplexer        │                              │
 •✓ Fix the flaky …  │                              │
  ! tests            │                              │
```

Two sidebar panels, split half and half and divided by a rule: the workspaces
you could be in (with their git branch) stay at the top, the agents inside them
are pinned to the bottom, grouped by workspace — so the agent list growing
doesn't shove the workspaces around. A `•` in the gutter marks an unread result.

The selected workspace and the focused agent are marked with a full-width band
rather than by colouring their text, and the band covers a workspace's branch
line too so a two-line entry reads as one block. It replaced an underline,
which at this size read as a stray rule under the text rather than as "you are
here". The colour is `colors.selection_bg`.

A workspace **follows the directory you are in**: `cd` somewhere else and its
name and git branch change with it (`~` at home). Name one yourself with
`rook workspace rename` or `rook workspace new <name>` and the name sticks —
but the directory and branch keep tracking, because a name is a choice and a
stale branch is just wrong.

Plain terminals are deliberately **not** in the agents panel: it answers "who
needs me", and a shell sitting at a prompt is not an answer.

Agents are named by **what they are doing**, read from the terminal title they
set — "Refactor the parser", not four rows all reading "claude". The spinner
frame is stripped (it is already shown as the status glyph), and a title that
is really the shell's — a path or a command line, which is what you see before
an agent's UI comes up — is ignored in favour of the program name. Set a fixed
name with `rook pane rename <pane> <name>`; a manual label always wins.

The top strip is tabs, numbered and coloured by what their agents want. There
is no permanent status bar: the help line appears while the prefix is pending
and disappears again, so the terminal keeps the screen.

Every pane carries a status glyph, in the sidebar and in its own header. A
workspace row carries one too — its *aggregate* status, so a workspace with a
blocked agent inside looks different from a merely idle one even with colour
turned off:

| Glyph | Status | Meaning |
| --- | --- | --- |
| `▖▘▝▗` | working | animated — the agent is running a turn |
| `` | blocked | it is asking for input or confirmation — it wants you |
| `` | done | it finished and you have not looked yet |
| `` | idle | it finished and you have looked |
| `` | unknown | not a recognised agent (a shell, a server, a test run) |

### Icons

Glyphs come from a theme, set in `config.json`:

```json
{ "ui": { "icons": "nerd", "spinner": "dots" } }
```

`icons` is `unicode` (default — plain symbols that render in any font at a
predictable width), `nerd` (needs a Nerd Font installed), or `ascii`. The Nerd
Font glyphs were picked by reading the cmap of 0xProto Nerd Font Mono rather
than from a cheat sheet, so they are all present and all exactly one cell wide
— but a font-independent default is the safer one, and the extra icons were
mostly decoration next to the status glyph that carries the meaning.

`spinner` is `dots` (default), `braille`, `bar`, `shade`, `line` or `pulse`.
The default is quadrant blocks `▖▘▝▗` rather than the familiar braille
`⠋⠙⠹` for a concrete reason: **many Nerd Fonts, 0xProto included, ship no
braille block at all**, so those frames come from a fallback font at a
different width and make the sidebar jitter once per frame. If your font does
have braille, `"spinner": "braille"` gives you the classic one.

The animation frame is derived from the wall clock on both sides, which is how
the pane headers (drawn by the daemon) and the sidebar (drawn by the client)
stay in step without exchanging a single message about it. The clock only runs
while something is actually working.

### Dim text

Agent TUIs draw everything they don't want read as content in faint — SGR 2:
Claude Code's input placeholder, its hints, the quiet half of a diff. The
emulator underneath rookery tracks no faint attribute at all, so all of that
arrived looking exactly like text you had typed yourself.

Faint now rides in on the one attribute bit the emulator does keep and nothing
here uses (blink) and is translated back to SGR 2 on the way out, so your
terminal does the dimming — which is the only way to get it right, since faint
is a blend towards the background rather than a fixed grey. A program that
genuinely wants blinking text gets faint text instead; nothing in a pane has
ever asked.

Measured, not assumed: `internal/termgrid/testdata/claude-welcome.raw` is a real
capture of Claude Code v2.1.220 writing to a PTY — 17 requests for faint, 21
`SGR 22`s clearing it — and the test replays it at five chunk sizes. That is
what caught the second bug: the emulator keeps no state for a multi-byte
character split across two writes, so a `╭` straddling a read boundary was lost
along with the rest of its box. A byte-at-a-time replay used to lose 7 of 32 dim
cells; now every chunk size renders identically.

### Pane borders

```json
{ "ui": { "pane_borders": "auto" } }
```

`auto` (default) boxes each pane once more than one shares a tab, `always` even
boxes a lone one, `never` falls back to a single header row per pane.

```
╭ 1 ▶ Refactor the parser ──────╮ ╭ 2 ! tests ───────────────────╮
│ ✳ Cogitating… (12s · esc to   │ │ Run failing test again? (y/n)│
│                               │ ╰──────────────────────────────╯
```

The title lives in the top edge, so a box costs one row rather than two, and
the focused pane's border is drawn in the accent colour — a far clearer "you
are here" than a bold header. A lone pane gets no box: there is nothing to
separate it from, and the two columns are better spent on content.

### Git

`prefix G` opens a git UI in a new pane, in the active workspace's directory —
whichever of `lazygit`, `gitui` or `tig` you have, falling back to a shell with
`git status` already printed. `rook git` does the same from a script.

rookery deliberately does not implement its own git UI. lazygit is better than
anything a multiplexer would grow on the side, and a pane is exactly the right
place to put it.

### Attention blink

A pane's border flashes for four seconds when its agent finishes. The badge
answers "which agent wants me" when you are looking elsewhere; the blink is
for the case where the result lands on the screen you are already staring at.

```json
{ "ui": { "blink": false } }
```

The phase comes from the wall clock, so the daemon's borders and the client's
chrome flash together without coordinating, and the repaint only runs while a
visible pane is inside its blink window.

### The manager bar

A row above the status line, always on screen. `prefix :` (or `prefix a`, or a
click on the bar) puts the cursor in it; type what you want and it goes to a
**manager agent** — an ordinary pane running your agent of choice, in its own
tab, with rookery's CLI on its PATH:

```
◆ ❯ split a pane and run the tests, tell me if they fail▌
```

`rook manager <request...>` does the same from a shell.

The bar spins while the manager is thinking, then holds its answer until the
manager says something else — so a one-line question needs no trip to the
manager's tab, and the reply is not overwritten by the next status message.
Longer answers are in the tab in full.

It costs one permanent row of pane height. That is deliberate: a bar that
existed only while you typed at it made every answer look like a status message
that had already scrolled away.

The manager is started on first use and briefed once: it is told it lives
inside rookery and given the commands that matter (`rook pane new`,
`pane send`, `pane read`, `wait agent-status`). Nothing about it is special
beyond rookery remembering which pane it is — it has the same CLI and the same
permissions as any agent you start yourself. It opens in its own tab and does
not steal focus, so asking it something never rearranges what you were
looking at.

Two things about typing at an agent that took measuring to get right, and which
`rook pane send` now does for every pane, not just the manager:

- **The Enter is a separate keypress**, 150ms after the text. Agent TUIs detect
  pastes by how bytes arrive: text and its carriage return in one write look
  like pasted content, so the CR lands *in* the composer as a newline and
  nothing is submitted.
- **Messages queue until the agent is ready.** A freshly spawned agent shows no
  "working" marker while its UI is still coming up, so it looks idle — and text
  typed into a composer that does not exist yet is simply lost. The manager
  drains one message per idle turn, which also stops two messages arriving
  concatenated.

```json
{ "ui": { "manager_cmd": "claude" } }
```

### Colours

Every colour is themeable; anything you leave out keeps its default:

```json
{ "ui": { "colors": {
  "accent": "#7aa2f7", "blocked": "3", "done": "2", "working": "6",
  "badge_bg": "3", "sidebar_bg": "235", "border": "238"
} } }
```

Values are ANSI indices (`"6"`), 256-colour indices (`"110"`), or hex
(`"#7aa2f7"`). Status colours default to low ANSI slots so they follow the
theme your terminal already uses. The client passes the accent, header and
border colours to the daemon in its `hello`, so the pane headers and split
dividers the daemon draws match the sidebar the client draws.

### Unread badges

A finished-but-unseen agent raises a count badge in three places: the workspace
row, the `agents` heading, and the right-hand end of the tab strip (so it is
visible even with the sidebar hidden). Zero never renders — a badge showing
"0" is just noise that never goes away.

Badges clear themselves as soon as the pane is **on screen** — switching to its
tab is enough, you don't have to focus that exact pane. You can read a pane you
aren't typing into, so visibility is what "seen" means. Reading a pane over the
API counts too, which is how one agent collecting a sibling's output stops that
sibling from nagging you.

### Integrations — letting agents report themselves

Screen detection is a heuristic. An integration replaces it with facts:

```bash
rook integration status            # what is installed, which agents are on PATH
rook integration install claude    # add the hooks
rook integration uninstall claude
```

For Claude Code, its own hook events map exactly onto rookery's three states:

| Hook | Status | Why |
| --- | --- | --- |
| `UserPromptSubmit` | working | a turn started |
| `Stop`, `StopFailure` | idle | the turn ended |
| `PermissionRequest` | blocked | waiting on you to allow something |
| `Elicitation` | blocked | an MCP server wants input |
| `Notification` | blocked | Claude Code raised a notification |
| `SessionStart` | idle | it is up and waiting |

`PermissionRequest` *is* blocked — no spinner to spot, no dialog wording to
match, nothing to be fooled by. A fresh report wins over screen detection for
that pane; if the reports stop (an agent killed mid-turn) rookery falls back to
reading the screen rather than leaving the pane stuck busy.

The installer merges into your existing `settings.json`: your own hooks are
kept, running it twice does not duplicate anything, and uninstall removes only
what rookery added. Verified against a copy of a real config with 26 hook
entries across 10 events — install/uninstall round-trips byte-identical. A
settings file it cannot parse is refused, not replaced.

#### Several configurations at once

Having more than one live config is normal — a relocated config directory for
work, another for personal, plus whatever the repo carries — and writing hooks
into one the agent never loads is the worst outcome, because it reports success
and changes nothing. So the default target is whatever the agent itself would
load (`$CLAUDE_CONFIG_DIR` if set, else `~/.claude`), and `status` lists every
config it can find with the active one marked:

```bash
rook integration status                              # every config found
rook integration install claude                      # the active one
rook integration install claude --config-dir ~/claude-personal
rook integration install claude --project            # ./.claude/settings.json
rook integration install claude --local              # ./.claude/settings.local.json
rook integration install claude --settings /path/to/settings.json
rook integration install claude --all                # every config found
```

`rook skill --install` takes the same flags.

Under the hood it is one call, so any agent can report without an installer:

```bash
rook report --status blocked --agent myagent
```

### The agent skill

```bash
rook skill              # print it
rook skill --install    # write it into the config the agent actually loads
rook skill --install --config-dir ~/claude-personal
rook skill --install --all
```

An agent dropped into a pane has the CLI on its PATH and `ROOK_ENV=1` in its
environment, and no reason to think either matters. The skill is what turns
"there is a rook binary here" into "I can hand this task to a sibling and wait
for it" — how to spawn agents without stealing focus, how to wait instead of
poll, how to fan a task out, and the etiquette of not killing panes it did not
create.

Same idea as Herdr's `SKILL.md`, and the same gate: it tells the agent to do
nothing unless `ROOK_ENV=1`.

### Agent detection rules

Status comes from prioritised rules in per-agent manifests, the way Herdr does
it. The built-in set is embedded, so it works with nothing on disk:

```bash
rook agents ls              # known agents, their executables, rule counts
rook agents show claude     # one manifest, rules and all
rook agents init            # copy the built-ins to ~/.rook/agents to edit
```

A file in `~/.rook/agents/*.json` with the same `id` as a built-in replaces it,
so you can correct a rule rather than fight it, and a new file adds an agent
rookery has never heard of:

```json
{
  "id": "mycoder",
  "exec": ["mycoder", "mc"],
  "rules": [
    {"id": "busy", "state": "working", "priority": 100,
     "region": "bottom", "contains": ["crunching"]}
  ]
}
```

`region` is `title` or `bottom`; a rule matches on `regex`, on `contains` (all
of them), on `any` (at least one), or a combination. Highest priority wins, and
an agent's own rules are considered alongside the shared `generic` ones. Blocked
markers sit above working ones on purpose: a confirmation dialog can have a
spinner running behind it, and your attention is what the status is for.

These are JSON rather than Herdr's TOML for one reason: every other rookery
config file is JSON and `encoding/json` is in the standard library. A TOML
dependency to read one directory is not a trade worth making.

A malformed file costs that file's rules and nothing else — the daemon still
starts, and says which file it skipped. The daemon reads them once at startup,
so restart it after editing.

### Sound

```json
{ "ui": { "sound": { "mode": "system", "min_interval_ms": 1500,
                     "done_path": "", "blocked_path": "" } } }
```

`mode` is `system` (default — a platform sound via `afplay`, `paplay`, `pw-play`,
`aplay` or `ffplay`), `bell` (the terminal bell, rung by the attached client),
or `off`. Point `done_path` / `blocked_path` at your own audio files to
override; a path that doesn't exist falls back rather than going silent.

When your terminal does **not** have focus, the ping is joined by an OS
notification — `terminal-notifier` or `osascript` on macOS, `notify-send` on
Linux — because a sound you are not there to hear is no notification at all.
The client reports focus and blur (terminal focus reporting, DECSET 1004), so
when you are looking at the terminal you get the sound and nothing else. A
terminal that cannot report focus is treated as focused, since a missing
banner beats a stream of unwanted ones.

```json
{ "ui": { "sound": { "desktop": true } } }
```

It pings for exactly two transitions — an agent that just got **blocked**, and
one that **finished with nobody watching**. Anything else would train you to
ignore it. Repeats inside `min_interval_ms` are dropped, so five agents
finishing together give you one ping rather than five.

The **daemon** plays it, not the client, so you still get pinged after
detaching — which is when you most want to know an agent got stuck.
`ROOK_DISABLE_SOUND=1` silences it for a run.

`done` and `idle` are the same underlying state; the difference is whether the
result has been seen. Focusing a pane, or reading it over the API, marks it
seen. That is what makes "which of my agents wants me?" answerable at a glance.

## Mouse

Mouse capture is on by default, the way Herdr ships it. Click a pane to focus
it, a tab or workspace to switch, an agent in the sidebar to jump straight to
it wherever it lives. Drag a divider to resize. Scroll to move back through the
pane's own history (see [Scroll and copy](#scroll-and-copy)). Right-click a tab
to rename it — the same prompt `prefix T` opens, on the tab you clicked rather
than the active one.

Panes whose program asked for mouse reporting — Claude Code, vim, anything
full-screen — get the events forwarded SGR-encoded in pane-local coordinates
instead, so their own click targets keep working.

While capture is on, your terminal's native text selection needs **shift+drag**
(the same trade tmux makes). Turn capture off with `prefix m`, or permanently:

```json
{ "ui": { "mouse_capture": false } }
```

## Scroll and copy

`prefix [` (or the wheel over a pane that hasn't claimed the mouse) scrolls
back through what that pane printed. The pane's header says how far back you
are, because a screen that has stopped updating otherwise looks like a hung
program.

| Key | |
| --- | --- |
| `j` `k` / arrows | move a line |
| `ctrl+d` `ctrl+u`, `f` `b`, page up/down | move a page |
| `g` / `G` | first / last line |
| `v` or space | start a selection |
| `y` or enter | copy — the cursor's line, or the selection |
| `esc` `q` | back to the live screen |

Anything else leaves scroll mode and goes to the program in the pane, so
wheeling back and then typing does what you meant rather than swallowing the
keystroke. Scrolling down past the last line also returns you to live.

Copying goes through **OSC 52**, so it reaches your real clipboard even when
the multiplexer is at the far end of an SSH connection — no clipboard daemon,
no X forwarding.

Two deliberate limits: the history is the plain-text transcript the daemon
already keeps (2000 lines, no colour, long lines cut at the pane's width — the
copied text is the full line), and selection is by whole lines. Cell-accurate
scrollback would mean an emulator that keeps styled history, which is a
different program; `rook pane read --scrollback` is the same data for agents.

## Go to

`prefix f` (or `/`) opens a fuzzy list of everything you can jump to: every
agent in every workspace, then the workspaces, then the tabs. Type a
subsequence — `rfp` finds "Refactor the parser" — and press enter.

The sidebar is fine at four agents. `rook fan --agents 8` is one command.

## Keys

`ctrl+b` is the prefix; `prefix ?` shows this list in the app, generated from
your own keymap.

| Key | Action |  | Key | Action |
| --- | --- | --- | --- | --- |
| `c` | new tab | | `N` | new workspace |
| `X` | close tab | | `w` / `W` | next / previous workspace |
| `n` / `p` | next / previous tab | | `D` | close workspace |
| `1`–`9` | jump to tab | | `b` | toggle sidebar |
| `T` | rename tab | | `g` | keyboard sidebar navigation |
| `[` / `esc` | scroll & copy mode | | `f` / `/` | go to (fuzzy) |
| `v` / `-` | split right / down | | `m` | toggle mouse capture |
| `h` `j` `k` `l` | move focus | | `?` | help |
| `H` `J` `K` `L` | resize | | `q` | detach |
| `z` | zoom pane | | `ctrl+b` | send a literal `ctrl+b` |
| `x` | close pane | | | |

Arrows work anywhere the `hjkl` do. Everything is remappable in
`~/.rook/hotkeys.json`, the prefix included:

```json
{ "prefix": "ctrl+a", "bindings": { "split_right": ["v", "|"] } }
```

Both config files are written with their defaults on first run, so the file
itself documents what can be changed. New actions added by a later build are
merged into an existing file rather than overwriting your bindings.

## Fan-out

One prompt, several agents, one git checkout each:

```bash
rook fan "make the flaky auth test pass" --agents 3
rook watch --status done,blocked      # tell me when they land
rook fan ls                           # who did what
git merge rook/fan1-2                 # keep the one you liked
rook fan clean fan1 --force           # bin the rest
```

Each agent gets its own tab, its own worktree under
`~/.local/state/rookery/worktrees`, and its own branch `rook/<name>-<n>`. Three
agents on one task therefore cannot fight over the index, and comparing their
answers is a diff rather than three transcripts.

```
fan1       w1:p2     done      rook/fan1-1   2 files changed, 31 insertions(+)
fan1       w1:p3     blocked   rook/fan1-2   1 file changed, 4 insertions(+)
fan1       w1:p4     done      rook/fan1-3   1 new file(s)
```

Untracked files are counted separately on purpose: `git diff` ignores them, so
an agent whose whole contribution was a new file used to look like it had done
nothing.

`fan clean` is all-or-nothing. If any worktree has uncommitted work it removes
nothing and says which — an earlier version closed the panes first and *then*
refused, which left checkouts with no way back to them. `--force` discards.

The prompt is **queued**, not typed immediately. An agent that has not finished
starting loses whatever you send it, which is how a fan-out of five quietly
becomes a fan-out of two; the queue drains one message per agent per idle turn.

## Watching

```bash
rook watch                                   # NDJSON, one event per line
rook watch --status done,blocked --plain     # human-readable
rook watch --pane w1:p2 --kind agent_status
```

```
12:50:45  w1:p1   one   pane_new
12:50:45  w1:p1   one   working → idle
12:50:47  w1:p1   one   idle → working
12:50:50  w1:p1   one   working → idle
```

Events: `agent_status` (with `previous` → `status`), `pane_new`, `pane_closed`,
`pane_exit`. Each line is flat JSON, so every field is one `jq` hop away:

```bash
rook watch --status blocked | while read -r ev; do
  say "$(jq -r .label <<<"$ev") needs you"
done
```

This exists to delete polling. An outer agent or a CI job can block on the
moment an agent finishes instead of asking `pane ls` every second and hoping to
catch it. Events are dropped rather than queued without bound if a consumer
falls behind — a slow pipeline must never be able to stall the multiplexer, and
the drop is counted rather than hidden.

## Sessions

| Command | What it does |
| --- | --- |
| `rook` | attach to the default session, starting it if needed |
| `rook serve [--session NAME] [-f]` | start the daemon (`-f` stays in the foreground) |
| `rook attach [session]` | attach a TUI |
| `rook ls` | list sessions and pane counts |
| `rook ping` / `rook kill [session]` | liveness check / stop a session |

### The layout survives a restart

Restarting the daemon is routine — a new binary changes nothing until the old
one goes away, which is exactly what `just install` does — and it used to take
every workspace, tab and split with it. The tree is now saved to
`~/.local/state/rookery/<session>/layout.json` whenever it changes, and read
back when the daemon starts.

What comes back: workspaces (names, directories, branches), tabs, the split
tree with its ratios, which pane was focused, which tab was active. Panes come
back as **a shell in the directory they were in** — not as whatever was running
in them. Relaunching eight agents (or a `tail -f`, or a half-finished
`git rebase`) because a daemon restarted is not a decision a multiplexer should
make for you. The structure is the tedious part; that is the part restored.

The file is written only when the structure actually changes, so a session full
of chatty agents does not rewrite it four times a second. Delete it to start
clean.

## Agents

Every `rook pane` and `rook wait` subcommand prints the raw API result as
indented JSON, so a tool call can parse it without a second output dialect.

```bash
rook workspace ls                               # workspaces, branch, rolled-up status
rook workspace new review --cwd ~/code/thing
rook tab ls ; rook tab new logs                 # tabs inside the active workspace
rook pane ls                                    # visible panes (--all for every one)
rook pane new --label reviewer --no-focus -- claude
rook pane send p2 review the current diff       # types the text, then Enter
rook wait agent-status p2 --status done,blocked --timeout 120000
rook pane read p2 --scrollback --lines 60 --raw
rook pane status p2 ; rook pane focus p2 ; rook pane rename p2 critic
rook pane kill p2
rook api pane.list                              # escape hatch for any method
```

`rook wait` is the piece that makes orchestration work: it blocks in the
daemon until the pane reaches one of the wanted states, so no polling loop is
needed. A pane already in a wanted state matches immediately. Exit status is 0
on a match and 1 on timeout, so this chains:

```bash
rook pane send p2 run the test suite
rook wait agent-status p2 --status done,blocked --timeout 300000 \
  && rook pane read p2 --scrollback --lines 80 --raw
```

Waiting also works for plain commands, not just agents — the daemon asks the
kernel which process group owns the PTY, so a shell running `sleep 30` is
`working` even though it prints nothing:

```bash
rook pane send p3 npm test
rook wait agent-status p3 --status idle --timeout 600000
```

Flags work on either side of the pane id (`rook pane read p1 --raw` is fine).
Everything after `--` reaches the wrapped command untouched, so an agent CLI's
own flags survive: `rook pane new -- claude --dangerously-skip-permissions`.

`--session NAME` targets a session; otherwise `$ROOK_SESSION`, otherwise
`default`. Every pane gets `ROOK_SESSION`, `ROOK_PANE` and `ROOK_ENV=1`, so an
agent *inside* a pane can drive rookery with no configuration:

```bash
test "${ROOK_ENV:-}" = 1 || echo "not running inside rookery"
rook pane current                                  # which pane am I?
rook pane new --current --direction right -- claude   # a sibling next to me
```

## How it fits together

| Package | Role |
| --- | --- |
| `internal/state` | the daemon's brain — one goroutine owns all mutation, everything else pushes events onto channels |
| `internal/state/workspace.go` | workspaces and tabs, plus the git branch read straight from `.git/HEAD` |
| `internal/state/layout.go` | the binary split tree: rectangles, dividers, directional focus |
| `internal/config` | `~/.rook/config.json` and `hotkeys.json`, defaults written on first run |
| `internal/agentstatus` | prioritised rules over the terminal title and the bottom of the screen |
| `internal/termgrid` | VT emulation, a cell canvas panes composite onto, scrollback |
| `internal/pty` | one spawned process per `Actor`, plus the foreground-process-group check |
| `internal/apiserver` + `internal/apiproto` | the JSON-RPC control socket agents use |
| `internal/attachserver` + `internal/attachproto` | the attach/render socket TUI clients use |
| `internal/tui` | the Bubble Tea client (the only package importing Bubble Tea) |
| `internal/session` | per-session socket/pid/log paths under `~/.local/state/rookery/<name>/` |

Panes are composited by the daemon at the cell level and sent as one ANSI
frame per tick; the client draws that frame and its own chrome. Both sockets
speak NDJSON — one JSON object per line — so `nc -U` is a valid debugging
client.

Not in this version: copy mode and drag-to-select, right-click menus, layout
persistence across a daemon restart, plugins, git worktrees, OS notifications,
and per-client viewports (the daemon renders one frame at the last attacher's
size). A mouse wheel in a pane that hasn't asked for mouse reporting sends
arrow keys rather than scrolling a real scrollback buffer. See
`future-plan.md`.
