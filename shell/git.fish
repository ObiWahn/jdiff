
# jdiff log and jdiff diff take git's arguments: after them, git's completion
# takes over, as for git log and git diff. Before them, the completion above
# runs.

# __jdiff_git_args prints the words from log or diff on, when they follow
# jdiff's flags on the command line, and fails else.
function __jdiff_git_args
    set -l words (commandline -opc)
    set -e words[1]
    while set -q words[1]
        switch $words[1]
            case log diff
                printf '%s\n' $words
                return 0
            case --features
                set -e words[1]
            case '-*'
            case '*'
                return 1 # OLD NEW
        end
        set -e words[1]
    end
    return 1
end

# __jdiff_git_complete prints git's completions of the command line from log
# or diff on.
function __jdiff_git_complete
    complete -C (string join ' ' git (string escape -- (__jdiff_git_args)) (commandline -ct))
end

# Cobra's completions only before log and diff.
functions -c __jdiff_prepare_completions __jdiff_prepare_cobra_completions
function __jdiff_prepare_completions
    __jdiff_git_args >/dev/null; and return 1
    __jdiff_prepare_cobra_completions
end
complete -c jdiff -n '__jdiff_git_args >/dev/null' -f -a '(__jdiff_git_complete)'
