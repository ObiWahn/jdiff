
# jdiff log and jdiff diff take git's arguments: after them, git's completion
# takes over, as for git log and git diff, when it is installed. Before them,
# and without it, the completion above runs.

# __jdiff_git_loaded reports whether git's completion is loaded, loading it
# as bash-completion does on demand.
__jdiff_git_loaded()
{
    declare -F __git_func_wrap >/dev/null 2>&1 && return
    if declare -F _comp_load >/dev/null 2>&1; then
        _comp_load git >/dev/null 2>&1
    elif declare -F __load_completion >/dev/null 2>&1; then
        __load_completion git >/dev/null 2>&1
    fi
    declare -F __git_func_wrap >/dev/null 2>&1
}

__jdiff_complete()
{
    # The word log or diff, after jdiff's flags.
    local i
    for ((i = 1; i < COMP_CWORD; i++)); do
        case ${COMP_WORDS[i]} in
        log | diff) break ;;
        --features)
            ((i++))
            [[ ${COMP_WORDS[i]} == = ]] && ((i++))
            ;;
        -*) ;;
        *) i=$COMP_CWORD ;; # OLD NEW
        esac
    done
    if ((i >= COMP_CWORD)) || ! __jdiff_git_loaded; then
        __start_jdiff
        return
    fi
    # The words from log or diff on, as the command line of a git alias.
    local line=$COMP_LINE point=$COMP_POINT j rest
    for ((j = 0; j <= i; j++)); do
        rest=${line#"${line%%[![:space:]]*}"}
        ((j < i)) && rest=${rest#"${COMP_WORDS[j]}"}
        ((point -= ${#line} - ${#rest}))
        line=$rest
    done
    local COMP_WORDS=("${COMP_WORDS[@]:i}") COMP_CWORD=$((COMP_CWORD - i)) COMP_LINE=$line COMP_POINT=$point
    compopt -o nospace 2>/dev/null # git adds the spaces itself
    __git_func_wrap "_git_${COMP_WORDS[0]}"
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __jdiff_complete jdiff
else
    complete -o default -o nospace -F __jdiff_complete jdiff
fi
