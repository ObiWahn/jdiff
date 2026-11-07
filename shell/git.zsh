
# jdiff log and jdiff diff take git's arguments: after them, git's completion
# takes over, as for git log and git diff, when it is installed. Before them,
# and without it, the completion above runs, renamed to _jdiff_cobra.
_jdiff()
{
    local i
    for (( i = 2; i < CURRENT; i++ )); do
        case $words[i] in
        log|diff)
            if (( $+functions[_git] )); then
                # The words from log or diff on, as git's.
                local service=git
                words=(git "${(@)words[i,-1]}")
                (( CURRENT -= i - 2 ))
                _git
                return
            fi
            break ;;
        --features) (( i++ )) ;;
        -*) ;;
        *) break ;; # OLD NEW
        esac
    done
    _jdiff_cobra "$@"
}

# don't run the completion function when being source-ed or eval-ed
if [ "$funcstack[1]" = "_jdiff" ]; then
    _jdiff "$@"
fi
