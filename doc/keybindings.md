# Key bindings

`?` shows all key bindings in a popup. To change them, see [keybindings.toml](config.md#keybindingstoml).

## By page

### Commit list (`jdiff log`)

| Key | Action |
|---|---|
| `j` `k` (`down` `up`) | move |
| `N` `P`, `n` `p` | next, previous commit in history, also with `--reverse` |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | page up, down |
| `home` `end` | first, last commit |
| `enter` | show the files of the commit |
| `'<letter>` | a mark: says to open a commit to jump to it |
| `f` | flow view of all commits |

### File list

| Key | Action |
|---|---|
| `j` `k` (`down` `up`), `n` `p` | move |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | scroll the diff preview; page up, down while it is hidden |
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
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`), `ctrl+u` `ctrl+d` | page, half page; the cursor keeps its screen row |
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
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | page up, down |
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
| `u` | in `jdiff diff`, [also show untracked files](#untracked-files); not saved |
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

### Untracked files

`u` also shows the untracked files that are not ignored, as added, and lists the files anew.
Unlike the other toggles it is not saved: each run starts without them, `jdiff -u diff` (`--untracked`) with them.

- They show where `git diff` compares with the working tree: `jdiff diff`, `jdiff diff HEAD`, not `--cached` or two commits.
- Your index stays as it is: jdiff marks them in a temporary copy of it, and the header says `+ untracked`.
- Directories, as nested repositories, and files that cannot be read, as broken symlinks, are left out.

### Settings popup

`o` or `F8` opens a popup with all the settings above, also `c` and `C` of the flow view, to find them.
Only there can you set the minutes until auto reload ends and the most directories watched (`inotify_max_dirs`), typed in a line after `enter` on them.

- `j` `k` move; `enter`, `space` or the letter toggles, kept in `state.toml`.
- The popup stays open; `o`, `F8`, `esc` or `q` close it.

### Auto reload

`R` keeps what is shown up to date: the files shown are reloaded when they change on disk, and in `jdiff diff` and `jdiff log` all is loaded again whenever `git status` changes, as with a commit, `git add`, a checkout, a new file or an edit of a file not yet in the diff.
The status bar says `auto reload` while it is on; off, nothing is watched nor checked.

- It runs `git status` every `auto_reload_interval` (10 seconds) and, on Linux, at once when the kernel reports a change of `HEAD`, the index, the refs of the current branch, or in a directory of the working tree: new files and edits of files not yet in the diff show at once, and the next regular check comes a full interval later.
- Of the working tree it watches the directories with tracked or untracked files, not ignored ones, nor those of version control (`.git`, `.svn`, `.hg` …) or nested repositories, up to `inotify_max_dirs` together with the files of the diff; changes elsewhere take up to `auto_reload_interval`. `HEAD` and the index are watched beyond that; with `inotify_max_dirs` 0 the working tree is not even looked at.
- Comparing two files or directories, it only watches the files shown: a file new in a directory still needs `r`, which reads all files.
- Changes of editors' files, as Vim's swap files, run no `git status`: see `watch_ignore`.
- After the minutes set in the settings (5) without a key pressed or a change it ends, and with it its git commands.
- It is not saved: off at each start.
