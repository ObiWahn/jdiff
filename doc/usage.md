# Usage

```sh
jdiff OLD NEW                 # compare two files
jdiff DIR1 DIR2               # compare two directories
jdiff log [GIT LOG ARGS]      # browse commits, e.g. jdiff log -20 or jdiff log main..topic
jdiff diff [GIT DIFF ARGS]    # what git diff shows: jdiff diff (unstaged), jdiff diff --cached (the index), jdiff diff HEAD; u, or -u before diff, adds the untracked files
jdiff --features NAMES ...    # delta features instead of delta.features, for the delta theme
```

`jdiff --version` prints the tag or commit it was built from, `jdiff --license` the license and those of the libraries linked into it, `jdiff --readme` shows the README rendered, and the pages of the documentation its links lead to: `enter` follows the link on the cursor's line, `backspace` goes back. When the output is no terminal, it prints the README as markdown, without its badge.

jdiff's flags come before `log` and `diff`: all arguments after them go to git as they are, so `jdiff -u diff` shows the untracked files, while `jdiff diff -u` hands `-u` to `git diff`, where it asks for a patch. Such flags, which change what git prints, as `-u`, `--stat` or `--oneline`, are refused: jdiff reads that output. `jdiff --help` lists the flags. To compare files named like a command, put `--` before them: `jdiff -- log log1`.

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

## Auto reload and file watching

What jdiff shows stays as it was loaded until you press `r`, or turn on auto reload with `R`.
While auto reload is on, edits you save show up live, and in `jdiff diff` and `jdiff log` also new files, commits and the like: see [auto reload](keybindings.md#auto-reload).
Watching files is part of auto reload, off with it, so that jdiff does nothing in the background while it is off.

- On Linux the kernel reports the changes (inotify): jdiff watches the directories the files are in, not their subdirectories, all with one file handle.
- It watches at most `inotify_max_dirs` (100) directories, those used most recently: where a change was seen, where the cursor was, else those modified last. `HEAD`, the index and the refs of the current branch are watched beyond that, always.
- Files in the other directories, and all of them on other systems, it checks every `watch_interval` (1 second) while the cursor is on them or next to them, and when the cursor comes to them.
- For a large repository, a small `inotify_max_dirs`, or 0, watches little more than `HEAD` and the index; `git status` then finds the rest every `auto_reload_interval`.
