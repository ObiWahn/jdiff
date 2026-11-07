# Usage

```sh
jdiff OLD NEW                 # compare two files
jdiff DIR1 DIR2               # compare two directories
jdiff log [GIT LOG ARGS]      # browse commits, e.g. jdiff log -20 or jdiff log main..topic
jdiff diff [GIT DIFF ARGS]    # what git diff shows: jdiff diff (unstaged), jdiff diff --cached (the index), jdiff diff HEAD; u, or -u before diff, adds the untracked files
jdiff --features NAMES ...    # delta features instead of delta.features, for the delta theme
```

`jdiff --version` prints the tag or commit it was built from, `jdiff --license` the license and those of the libraries linked into it, `jdiff --readme` shows the README rendered, and the pages of the documentation its links lead to: `enter` follows the link on the cursor's line, `backspace` goes back. When the output is no terminal, it prints the README as markdown, without its badge.

jdiff's flags come before `log` and `diff`: all arguments after them go to git as they are, so `jdiff -u diff` shows the untracked files and `jdiff diff -u` passes `-u` to `git diff`. `jdiff --help` lists the flags.

## Git aliases

In `~/.gitconfig`: `git jd` takes the arguments of `git diff` (`git jd --cached`, `git jd main..topic`), `git jl` those of `git log`:

```gitconfig
[alias]
	jd = "!f() { : git diff; cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jdiff = "!f() { : git diff; cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jl = "!f() { : git log; cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
	jlog = "!f() { : git log; cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
```

The null command `: git diff;` does nothing when the alias runs; git's bash completion reads it and completes `git jd` like `git diff`: options, branches and paths.
It has to come first in the function, before any other command.

## Git difftool

For `git difftool -d`:

```gitconfig
[diff]
	tool = jdiff
[difftool "jdiff"]
	cmd = jdiff \"$LOCAL\" \"$REMOTE\"
[difftool]
	prompt = false
```

## File watching

Files of the working tree are watched, so edits you save while jdiff is open show up live.
On Linux the kernel reports the changes (inotify): jdiff watches the directories the files of the diff are in, not their subdirectories, up to `inotify_max_dirs` (20) of them, all with one file handle.
Elsewhere, or when the files are in more directories, it checks the files every `watch_interval` (1 second).
