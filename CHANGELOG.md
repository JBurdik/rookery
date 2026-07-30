# Changelog

## Unreleased

### Added

- SSH-backed remote attach: `rook attach [session] --remote user@host` (or
  `rook --remote user@host [session]`) keeps Rook's sockets private on the
  remote machine while SSH carries the interactive terminal.
- Session lifecycle commands: `rook status`, `rook reload`, `rook delete`, and
  the `rook session` namespace. Reload applies daemon sound settings and agent
  manifests without stopping panes.
- Agent-safe pane automation: literal `pane send-text`, `pane send-keys`, and
  `pane run`; pane inspection, neighbour lookup, and movement between tabs;
  plus `wait output` with literal or regular-expression matching.
- Fan-out review and promotion: `rook fan review` summarizes committed work and
  can print a candidate patch; `rook fan promote` dry-runs by default and only
  fast-forwards a clean, committed candidate when passed `--apply`.
- `rook agents explain <pane>` reports the detector path, winning manifest rule,
  and live signals behind a pane's current agent status.
- Shell completions: `rook completion bash` and `rook completion zsh` print
  scripts for the supported interactive shells.
- xterm-style input encoding now covers modified navigation/editing keys and
  F1–F12, including Alt variants.
- First-run setup can now configure Claude Code, Codex CLI, and OpenCode
  integrations.

### Changed

- Removed the manager bar and its special manager-agent machinery. Input
  queuing remains a normal pane capability.
- Updated the documentation and bundled agent skill to match the current
  command surface.
