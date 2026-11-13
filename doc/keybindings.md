# Key bindings

`?` shows all key bindings in a popup. To change them, see [keybindings.toml](config.md#keybindingstoml).

## By page

### Commit list (`jdiff log`)

| Key | Action |
|---|---|
| `j` `k` (`down` `up`) | move |
| `N` `P`, `n` `p` | next, previous commit in history, also with `--reverse` |
| `pgup` `pgdn` | page up, down |
| `home` `end` | first, last commit |
| `enter` | show the files of the commit |
| `'<letter>` | a mark: says to open a commit to jump to it |
| `f` | flow view of all commits |

### File list

| Key | Action |
|---|---|
| `j` `k` (`down` `up`), `n` `p` | move |
| `pgup` `pgdn` | scroll the diff preview; page up, down while it is hidden |
| `home` `end` | first, last file |
| `enter` | show the diff of the file |
| `'<letter>` | open the file of a jump mark |
| `N` `P` | next, previous commit in history (in the log) |
| `f` | flow view of all files |
| `backspace` | back to the commits (in the log) |

### Diff view

| Key | Action |
|---|---|
| `enter` | mark a line, then its partner: anchor; on an anchored line: remove the anchor |
| `esc` | cancel the mark |
| `space` / `ctrl+space` | fold or unfold the nearest / all unchanged code |
| `j` `k` (`down` `up`) | move |
| `pgup` `pgdn`, `ctrl+u` `ctrl+d` | page, half page; the cursor keeps its screen row |
| `home` `end` | top, bottom |
| `h` `l` (`left` `right`) | scroll left, right, unless lines are wrapped |
| `]` `[` | next, previous change |
| `n` `p` | next, previous file |
| `N` `P` | next, previous commit in history (in the log) |
| `tab` | switch pane |
| `/` | [search](#search) |
| `.` `,` | next, previous match |
| `g` | go to the other end of moved code |
| `m<letter>` | set a jump mark on the line of the new file |
| `'<letter>` | jump to a mark, when its file is in this diff |
| `f` | flow view |
| `backspace` | back to the file list, also from the flow view |

### Flow view

The keys of the diff view, and:

| Key | Action |
|---|---|
| `n` `p` | next, previous file bar |
| `N` `P` | next, previous commit bar in history |
| `f` | single file view |
| `c` | full or short commit messages, in the log |
| `C` | stay in the current commit, in the log (`N` `P` still switch) |

### README (`jdiff --readme`)

| Key | Action |
|---|---|
| `j` `k` (`down` `up`) | move the cursor |
| `pgup` `pgdn` | page up, down |
| `ctrl+u` `ctrl+d` | half page up, down |
| `home` `end` | top, bottom |
| `tab` `shift+tab` | the next, previous link |
| `h` `l` (`left` `right`) | the link left, right on the cursor's line |
| `enter` | follow the link on the cursor's line: open its page or heading; a web address is shown |
| `backspace` | back to where the link was followed |
| `n` `p` | next, previous page |
| `/` | search the page |
| `.` `,` | next, previous match |

### Everywhere

| Key | Action |
|---|---|
| `\` | jump marks: `j` `k` move, `enter` jumps, `d` or `del` deletes, `\`, `esc` or `q` close |
| `d` | [show another `git diff`](#another-git-diff) |
| `s` | side by side or normal layout, also of the preview |
| `f` | flow view of all files or commits; in it, the single file view |
| `u` | in `jdiff diff`, [also show untracked files](#untracked-ignored-and-excluded-files); kept in `state.toml` |
| `i` | with `u`, also the files `.gitignore` ignores; kept in `state.toml` |
| `I` | with `u`, also the files `.git/info/exclude` ignores; kept in `state.toml` |
| `w` | wrap long lines or cut them |
| `W` | show all whitespace and line ends or only changed |
| `ctrl+w` | mark changed whitespace or not |
| `t` | show trailing whitespace or not |
| `e` | show all `\r\n` line ends (`␍`) or only changed |
| `b` | bold or plain changed words |
| `v` | show or hide the message box or diff preview |
| `o` `F8` | [settings popup](#settings-popup) |
| `r` `F5` `ctrl+r` | load again: run `git diff` or `git log` again, list the directories again or read the two files |
| `R` | [auto reload](#auto-reload) |
| `?` | help |
| `q` `ctrl+c` | quit |

## In popups and input lines

These keys are rows of [keybindings.toml](config.md#keybindingstoml) too, each popup and input line a table of its own. A popup looks a key up in its table, then in `popup`; an input line in its table, then in `input_line`. The key that opened a popup closes it too.

### All popups (`popup`)

| Key | Action |
|---|---|
| `esc` `q` | close the popup |
| `ctrl+c` | quit |
| `down` `up`, `j` `k` | move down, up |

### Jump marks (`marks_popup`)

| Key | Action |
|---|---|
| `enter` | jump to the mark |
| `d` `delete` | delete the mark |

### Settings (`settings_popup`)

| Key | Action |
|---|---|
| `enter` `space` | toggle the setting, or type its value |
| `K` | [list the keys](#settings-popup) of all pages, to change them |

### Patterns of ignored_hide (`ignored_hide`)

| Key | Action |
|---|---|
| `esc` `q` | back to the settings |
| `a` | add a pattern |
| `enter` | edit the pattern |
| `d` `delete` | delete the pattern |

### Keys (`keys_popup`)

| Key | Action |
|---|---|
| `esc` `q` | back to the settings |
| `enter` | take the keys pressed next as the keys of the row |
| `a` | add the key pressed next to the row |
| `d` `delete` | give the row its built-in keys |
| `D` | give all rows their built-in keys, the file kept as `.bak` |

### A key pressed for a row (`key_capture`)

| Key | Action |
|---|---|
| `esc` | cancel; every other key is taken |

### Questions (`confirm`)

| Key | Action |
|---|---|
| `y` | yes |
| `n` `esc` | no |
| `ctrl+c` | quit |

### Input lines (`input_line`)

| Key | Action |
|---|---|
| `enter` | take what is typed |
| `esc` | cancel |
| `backspace` | erase a character; on nothing typed cancel |
| `ctrl+w` | erase a word |
| `ctrl+u` | erase the line |
| `ctrl+c` | quit |

### Another git diff (`git_diff_line`)

| Key | Action |
|---|---|
| `enter` | show the files of `git diff ARGS` |
| `up` | the current ARGS, to edit |

### Search (`search_line`)

| Key | Action |
|---|---|
| `enter` | find |
| `tab` | changed or all lines |
| `shift+tab` | plain text or a regular expression |
| `up` `down` | earlier, later searches |

### While the terminal is too small (`too_small`)

| Key | Action |
|---|---|
| `ctrl+c` | quit |

## Details

### Search

`/` opens a bar at the bottom; type the text.

- `tab` switches between changed lines (added, removed and moved; the default) and all lines.
- `shift+tab` switches between plain text and a regular expression (Go syntax). Both choices are kept in `state.toml`.
- `↑` `↓` go through the earlier searches of this session.
- `enter` finds; `esc`, or `backspace` on nothing typed, cancels.

Case is ignored unless the text holds a capital letter.
Matches are highlighted; a match in folded code unfolds it, one beyond the pane's edge scrolls it into view.

`.` and `,` go to the next and previous match; past the last or first one the cursor stays.
They cover all files in the flow view, and in the log the files of the current commit: `N` `P` go to the other commits.

### Another git diff

`d` opens a popup that shows the files of `git diff ARGS` instead of what is shown, also instead of the log.
Type ARGS, e.g. `--cached` or `HEAD~3 HEAD`.

- `↑` brings the current ARGS to edit, while a `git diff` is shown.
- `enter` shows; `esc`, or `enter` or `backspace` on nothing typed, cancels.
- The popup grows with ARGS, up to the screen's width.
- When git fails, the popup stays and says why.

git runs in the directory jdiff was started in, also when it compares two directories or files.

### Untracked, ignored and excluded files

`u` also shows the untracked files that are not ignored, as added, and lists the files anew.
With `u` on, `i` adds the files the `.gitignore` files of the working tree ignore, and `I` those `.git/info/exclude` ignores; the global excludes file (`core.excludesFile`) is left out.
Each is kept in `state.toml`, so later runs show them too; `jdiff -u diff` (`--untracked`), `-i` (`--ignored`) and `-I` (`--excluded`) show them for that run only, without changing `state.toml`.

- They show where `git diff` compares with the working tree: `jdiff diff`, `jdiff diff HEAD`, not `--cached` or two commits.
- Your index stays as it is: jdiff marks them in a temporary copy of it.
- The line above the files names what shows: `WORK (U)`, `WORK (U/I)`, `WORK/INDEX (U/I/X)`. Each file says what it is after its counts, in the file list, its single file view and the flow view: `U` untracked, `I` ignored by `.gitignore`, `X` excluded by `.git/info/exclude`; one both ignore counts as `I`.
- `i` and `I` with `u` off say so and change nothing; `u` off hides them, `u` on brings them back.
- [`ignored_hide`](config.md) leaves out ignored and excluded files by their paths; by default the directories of tools, as `node_modules/`, `.venv/` or `__pycache__/`, at any depth. The [settings popup](#settings-popup) edits it; it is kept in `state.toml`, one pattern per line.
- While they show, auto reload also runs `git status` with the ignored files and watches their directories.
- Directories, as nested repositories, and files that cannot be read, as broken symlinks, are left out.

### Settings popup

`o` or `F8` opens a popup with all the settings above, also `c` and `C` of the flow view, to find them.
Only there can you set the minutes until auto reload ends and the most directories watched (`inotify_max_dirs`), typed in a line after `enter` on them, and edit the patterns of [`ignored_hide`](#untracked-ignored-and-excluded-files): `enter` on its row lists them, `a` adds one, `enter` edits the selected one, `d` or `del` deletes it, `esc` goes back to the settings. A pattern that is no regular expression, or an empty one, is refused with the reason; each change is saved in `state.toml` and lists the files anew.

- `j` `k` move; `enter`, `space` or the letter toggles, kept in `state.toml`.
- The popup stays open; `o`, `F8`, `esc` or `q` close it.

`K` there, or `enter` on its last line, lists the keys of all pages, row by row, with `*` at those your [keybindings.toml](config.md#keybindingstoml) changes:

- `enter` takes the key pressed next as the keys of the row; an action that goes two ways takes two, in the order of the help, as `]` `[` for next, previous change. `esc` cancels.
- `a` adds the key pressed next to the row, `d` or `del` gives the action on that page its built-in keys again.
- `D` gives all actions their built-in keys: after `y` to its question it moves `keybindings.toml` to `keybindings.toml.bak`, over an older one; `n` or `esc` keep it.
- Each change is saved in `keybindings.toml` and applies at once, on the pages, in the status bars, the settings and the help. The rest of the file stays as you wrote it.
- A change that would leave a key with two actions on one page, or a key twice in the rows of an action, is refused with the reason.
- `esc`, `q`, `o` or `F8` go back to the settings.

### Auto reload

`R` keeps what is shown up to date: the files shown are reloaded when they change on disk, and in `jdiff diff` and `jdiff log` all is loaded again whenever `git status` changes, as with a commit, `git add`, a checkout, a new file or an edit of a file not yet in the diff.
The status bar says `auto reload` while it is on; off, nothing is watched nor checked.

- It runs `git status` every `auto_reload_interval` (10 seconds) and, on Linux, at once when the kernel reports a change of `HEAD`, the index, the refs of the current branch, or in a directory of the working tree: new files and edits of files not yet in the diff show at once, and the next regular check comes a full interval later.
- Of the working tree it watches the directories with tracked or untracked files, not ignored ones, nor those of version control (`.git`, `.svn`, `.hg` …) or nested repositories, up to `inotify_max_dirs` together with the files of the diff; changes elsewhere take up to `auto_reload_interval`. `HEAD` and the index are watched beyond that; with `inotify_max_dirs` 0 the working tree is not even looked at.
- Comparing two files or directories, it only watches the files shown: a file new in a directory still needs `r`, which reads all files.
- Changes of editors' files, as Vim's swap files, run no `git status`: see `watch_ignore`.
- After the minutes set in the settings (15) without a key pressed or a change it ends, and with it its git commands.
- It is kept in `state.toml`: on, the next start starts it too. Its end after the minutes leaves it on there.
- `jdiff -R` (`--auto-reload`) starts it for that run only, without changing `state.toml`, and it does not end after the minutes; `R` then switches it as usual.
