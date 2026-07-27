---
name: rookery
description: "Control rookery, a terminal multiplexer for coding agents, from inside one of its panes. Use when the user asks to run work in parallel, hand a task to another agent, watch or wait on a sibling agent, split panes, or manage workspaces and tabs — and only when ROOK_ENV=1. Requires ROOK_ENV=1."
---

# rookery

rookery is a terminal multiplexer built for coding agents. A daemon owns the
terminals; you drive it with the `rook` CLI over a Unix socket.

Before doing anything, check that you are inside a rookery pane:

```bash
test "${ROOK_ENV:-}" = 1
```

If that fails, say you are not running inside rookery and stop. Do not try to
control a session you are not part of.

## Where you are

Every pane gets these:

```bash
echo "$ROOK_SESSION $ROOK_WORKSPACE $ROOK_TAB $ROOK_PANE"
```

Ids are hierarchical and stable: workspace `w1`, tab `w1:t2`, pane `w1:p3`. A
bare `p3` resolves inside the active workspace. Read ids out of command output
rather than constructing them — they are opaque strings.

Every command prints JSON. Parse it; do not scrape the human-readable lines.

## Learn the current surface

The installed binary is the authority, not this file:

```bash
rook --help
rook pane help
rook fan help
rook wait help
```

## The one thing to get right

**Agents take time to start, and text sent too early is lost.** `rook pane send`
writes immediately, which is right for a shell and wrong for an agent that is
still drawing its first frame. For an agent you have just spawned, wait for it:

```bash
rook pane new --label reviewer --no-focus -- claude
rook wait agent-status w1:p4 --status idle --timeout 60000
rook pane send w1:p4 review the diff on this branch and report only real problems
```

`rook fan` and the manager bar already queue for you. A pane you spawned
yourself does not.

## Working with other agents

```bash
rook pane ls                       # panes with agent and agent_status
rook pane new --label NAME --no-focus -- claude
rook pane send PANE some text      # types it, then Enter
rook pane read PANE --raw          # the visible screen
rook pane read PANE --scrollback --lines 200 --raw
rook pane status PANE
rook pane kill PANE
```

Statuses are `working`, `blocked` (it needs a human), `done` (finished, nobody
has looked), `idle` (finished, seen), `unknown` (not a recognised agent). Treat
`done` and `idle` as finished; the difference is only whether it has been seen.

`--no-focus` matters: spawning a pane should not move the user's focus away from
whatever they were doing.

## Waiting instead of polling

```bash
rook wait agent-status PANE --status done,blocked --timeout 300000
rook wait exit PANE --timeout 600000
```

Exit status is 0 on a match and 1 on timeout, so this chains:

```bash
rook wait agent-status w1:p4 --status done --timeout 300000 \
  && rook pane read w1:p4 --scrollback --lines 120 --raw
```

A pane already in a wanted state matches immediately. Never poll `pane ls` in a
loop — use `wait`, or stream events:

```bash
rook watch --status done,blocked      # NDJSON, one event per line
```

## Fanning one task across several agents

```bash
rook fan "make the flaky auth test pass" --agents 3
rook fan ls                           # status and diffstat per agent
git merge rook/fan1-2                 # keep the answer you liked
rook fan clean fan1 --force
```

Each agent gets its own git worktree and branch, so they cannot conflict and
their answers can be diffed. The prompt is queued and delivered as each agent
becomes ready.

## Layout, when asked

Default to a sibling pane in the current tab. Do not create workspaces, tabs or
worktrees unless the user asked for that shape.

```bash
rook pane new --current --direction right --no-focus -- claude
rook tab new logs
rook workspace ls
rook pane focus PANE                  # change what the human is looking at
rook pane rename PANE reviewer
```

Splitting a wide pane to the right and a tall one down keeps both usable;
rookery picks that for you when you omit `--direction`.

## Etiquette

- Do not `rook kill` the session or close panes you did not create.
- Do not move the user's focus without being asked — `--no-focus` by default.
- Prefer reading a sibling's screen over asking the user what it said.
- Reading a pane marks it seen, which clears its unread badge. That is correct
  when you have collected the result, and rude if you are just peeking.
