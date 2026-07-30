# Bash completion for rook. Install with:
#   source <(rook completion bash)

_rook()
{
    local cur prev command subcommand
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    command="${COMP_WORDS[1]}"
    subcommand="${COMP_WORDS[2]}"

    local root_commands="serve attach ls status reload delete rm session pane workspace ws tab api wait report integration skill setup fan watch git agents kill ping completion help version"
    local common_flags="--session"

    if (( COMP_CWORD == 1 )); then
        COMPREPLY=( $(compgen -W "$root_commands --help --version -v --remote" -- "$cur") )
        return 0
    fi

    case "$command" in
        completion)
            COMPREPLY=( $(compgen -W "bash zsh" -- "$cur") ) ;;
        serve)
            COMPREPLY=( $(compgen -W "--session --foreground -f --help" -- "$cur") ) ;;
        attach)
            COMPREPLY=( $(compgen -W "--remote -r --help" -- "$cur") ) ;;
        session)
            COMPREPLY=( $(compgen -W "ls attach status kill delete --help" -- "$cur") ) ;;
        workspace|ws)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "ls list new create focus rename close kill help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$common_flags --cwd --empty --help" -- "$cur") )
            fi ;;
        tab)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "ls list new create focus rename close kill help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$common_flags --workspace --cwd --empty --help" -- "$cur") )
            fi ;;
        pane)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "ls list new create split send run send-text send-keys read status focus inspect neighbor move rename zoom current kill close help" -- "$cur") )
            else
                case "$subcommand" in
                    new|create|split) COMPREPLY=( $(compgen -W "$common_flags --label --cwd --cols --rows --direction --from --no-focus --current --env --help" -- "$cur") ) ;;
                    send|run) COMPREPLY=( $(compgen -W "$common_flags --no-enter --help" -- "$cur") ) ;;
                    read) COMPREPLY=( $(compgen -W "$common_flags --scrollback --lines --ansi --raw --help" -- "$cur") ) ;;
                    neighbor) COMPREPLY=( $(compgen -W "$common_flags --direction left right up down --help" -- "$cur") ) ;;
                    move) COMPREPLY=( $(compgen -W "$common_flags --tab --from --direction right down --help" -- "$cur") ) ;;
                    zoom) COMPREPLY=( $(compgen -W "$common_flags --on --off --help" -- "$cur") ) ;;
                    *) COMPREPLY=( $(compgen -W "$common_flags --help" -- "$cur") ) ;;
                esac
            fi ;;
        wait)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "agent-status status exit output help" -- "$cur") )
            else
                case "$subcommand" in
                    agent-status|status) COMPREPLY=( $(compgen -W "$common_flags --status --timeout --current idle working blocked done --help" -- "$cur") ) ;;
                    exit) COMPREPLY=( $(compgen -W "$common_flags --timeout --current --help" -- "$cur") ) ;;
                    output) COMPREPLY=( $(compgen -W "$common_flags --match --regex --scrollback --timeout --current --help" -- "$cur") ) ;;
                esac
            fi ;;
        fan)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "ls list status clean review promote --agents --cmd --name --base --no-worktree --help" -- "$cur") )
            else
                case "$subcommand" in
                    ls|list|status) COMPREPLY=( $(compgen -W "$common_flags --json --help" -- "$cur") ) ;;
                    clean) COMPREPLY=( $(compgen -W "$common_flags --force --help" -- "$cur") ) ;;
                    review) COMPREPLY=( $(compgen -W "$common_flags --patch --json --help" -- "$cur") ) ;;
                    promote) COMPREPLY=( $(compgen -W "$common_flags --apply --help" -- "$cur") ) ;;
                    *) COMPREPLY=( $(compgen -W "$common_flags --agents --cmd --name --base --no-worktree --help" -- "$cur") ) ;;
                esac
            fi ;;
        integration)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "status ls list install uninstall remove help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$common_flags --config-dir --project --local --settings --all claude codex opencode --help" -- "$cur") )
            fi ;;
        agents)
            COMPREPLY=( $(compgen -W "ls list show init --help" -- "$cur") ) ;;
        watch)
            COMPREPLY=( $(compgen -W "$common_flags --status --pane --kind --plain --help" -- "$cur") ) ;;
        report)
            COMPREPLY=( $(compgen -W "$common_flags --status --session-ref --session-ref-stdin --agent --quiet --help" -- "$cur") ) ;;
        skill)
            COMPREPLY=( $(compgen -W "--install --help" -- "$cur") ) ;;
        ls|status|reload|delete|rm|kill|ping|git|api|setup)
            COMPREPLY=( $(compgen -W "$common_flags --help" -- "$cur") ) ;;
    esac
}

complete -F _rook rook
