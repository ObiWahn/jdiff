# jdiff

An interactive diff viewer for the terminal, side by side or as one column, with manual line alignment.

jdiff matches lines on their content, ignoring indentation, so re-indented code still lines up with its original.
Where the automatic alignment guesses wrong, you pick two lines and anchor them: everything around the anchor aligns again.
It reads [delta](https://github.com/dandavison/delta)'s git config, so an existing delta theme applies right away.

## Inspired by delta

jdiff is inspired by [delta](https://github.com/dandavison/delta), the syntax-highlighting pager for git written in Rust.
delta formats the output of git once and hands it to a pager; jdiff is a program you work in, so it can do what a pager cannot:

- **Resizing:** the diff is laid out again when the terminal changes size; delta lays it out once, for the width it started with.
- **Switching the layout:** two panes or one column like `git diff`; `s` switches between them while you look at the diff. In delta, side by side is chosen at startup.
- **Reflow:** `r` switches between wrapping long lines onto further lines, marked `↪`, and cutting them; delta wraps a line as often as `--wrap-max-lines` allows, set at startup, and truncates the rest.
  - A cut line that goes on past its pane ends in `»`, one scrolled out to the left (`h` `l`) starts with `«`.
- **More or less context:** long unchanged runs collapse to a single row; `space` opens or closes the nearest one, `ctrl+space` all of them. delta shows the context lines git gave it.
- **Walking the history:** `jdiff log` lists the commits, opens a commit's files one by one, and `N` `P` step to the next or previous commit, keeping the open file.
- **Following moved code:** like delta, jdiff colors blocks moved within a file or between the files of one commit or diff, magenta where they moved away and cyan where they moved to, as git's `--color-moved` does. `o` jumps to the other end, also into another file.
- **Auto reload:** outside the log, files on disk are watched: on Linux the kernel reports their changes (inotify), elsewhere, or when the files are in more than `inotify_max_dirs` directories, they are checked every second. When you save an edit, its diff updates while jdiff is open, and anchors, folds and the cursor follow their lines. `R` or `F5` runs `git diff` or `git log` again, or lists the directories again, so new files and commits show up too. delta shows the diff as it was when git ran.
- **Anchors:** mark a line with `enter`, then its partner in the other pane. jdiff realigns around it. Anchors that would cross are offered for replacement.
- **Jump marks:** `m` plus a letter marks a line of the new file, `'` plus the letter jumps back to it within the diff. `\` lists the marks; there, `enter` jumps and `d` deletes.
  - Marks are kept per repository and survive restarts.
  - A mark finds its line again by its text when the file changed.

delta highlights syntax; jdiff does not.

## What else it can do

- **Compare two files**, two directories (`git difftool --dir-diff`), what `git diff` shows (`jdiff diff`, e.g. `--cached` for the index), or the commits of `git log`.
- **Switch what is compared:** `d` asks in a popup for the arguments of `git diff`, e.g. `HEAD~3 HEAD`, and shows its files instead of what is shown, from any view, also from the log.
- **Word-level highlighting** of changed lines. Indentation changes are shown character by character, so only the added or removed part is marked.
- **Flow view (`f`):** all files of a diff, or all commits of the log, one after the other in one scrollable view. Commits load as they come near the cursor.
  - `c` shows the full commit messages or only their subjects.
  - `C` keeps the cursor inside the current commit.
- **Search:** `/` searches the diff, as plain text or a regular expression, `.` and `,` go to the next and previous match, in the flow view through all files, in the log within the current commit.
- **Long lists:** when the file or commit list, the help or the jump list goes on above or below, its first or last row says so, e.g. `↓ 21 more`; an arrow never stands for a single item, which is shown instead.
- **Previews:** the commit list shows the selected commit's message, the file list a preview of the selected file's diff (`v`).
- **Whitespace:** changed indentation, tabs and runs of spaces are marked as `·` and `→`, only their added or removed part, not single spaces between changed words; `w` draws all whitespace so and line ends as `¶` (`line_end`), `W` turns the marking of changed whitespace off, `t` shows trailing whitespace. Changed `\r\n` line ends show as `␍`, all of them with `e`. Invisible characters, among them those that change the text direction, show as `☠`.
- **16-color output:** every color is an ANSI color, so your terminal palette decides how jdiff looks.

## Install

jdiff needs Go 1.24 or newer.

```sh
make build        # builds bin/jdiff
go install .      # or install it into $GOBIN
```

Or download a binary from the releases, for Linux, macOS or Windows, and make it executable (`chmod +x`).

`jdiff --version` prints the tag or commit it was built from.

The other make targets: `make test`, `make vet`, `make vuln` (govulncheck), `make fmt`, `make fmt-check` and `make clean`.

## Usage

```sh
jdiff OLD NEW                 # compare two files
jdiff DIR1 DIR2               # compare two directories
jdiff log [GIT LOG ARGS]      # browse commits, e.g. jdiff log -20 or jdiff log main..topic
jdiff diff [GIT DIFF ARGS]    # what git diff shows: jdiff diff (unstaged), jdiff diff --cached (the index), jdiff diff HEAD
jdiff --features NAMES ...    # delta features instead of delta.features, for the delta theme
```

Git aliases, in `~/.gitconfig`: `git jd` takes the arguments of `git diff` (`git jd --cached`, `git jd main..topic`), `git jl` those of `git log`:

```gitconfig
[alias]
	jd = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jdiff = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jl = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
	jlog = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
```

As git's difftool, for `git difftool -d`:

```gitconfig
[diff]
	tool = jdiff
[difftool "jdiff"]
	cmd = jdiff \"$LOCAL\" \"$REMOTE\"
[difftool]
	prompt = false
```

Files of the working tree are watched, so edits you save while jdiff is open show up live. On Linux the kernel reports the changes (inotify): jdiff watches the directories the files of the diff are in, not their subdirectories, up to `inotify_max_dirs` (20) of them, all with one file handle. Otherwise it checks the files every `watch_interval`.

## Keys

`?` shows all key bindings in a popup.

### Commit list (`jdiff log`)

| Key | Action |
|---|---|
| `j` `k` | move |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | page up, down |
| `g` `G` | first, last commit |
| `enter` | show the files of the commit |
| `f` | flow view of all commits |
| `v` | show or hide the message box |

### File list

| Key | Action |
|---|---|
| `j` `k`, `n` `p` | move |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | page up, down |
| `g` `G` | first, last file |
| `enter` | show the diff of the file |
| `'`*letter* | open the file of a jump mark |
| `N` `P` | next, previous commit in history (in the log) |
| `f` | flow view of all files |
| `v` | show or hide the diff preview |
| `s` | side by side or normal preview |
| `w` | show all whitespace and line ends or only changed |
| `W` | mark changed whitespace or not |
| `t` | show trailing whitespace or not |
| `e` | show all `\r\n` line ends (`␍`) or only changed |
| `r` | reflow: wrap long lines or cut them |
| `b` | bold or plain changed words |
| `backspace` | back to the commits (in the log) |

### Diff view

| Key | Action |
|---|---|
| `j` `k` | move |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`), `ctrl+u` `ctrl+d` | page, half page; the cursor keeps its screen row |
| `g` `G` | top, bottom |
| `h` `l` | scroll left, right, unless lines are wrapped |
| `]` `[` | next, previous change |
| `n` `p` | next, previous file |
| `N` `P` | next, previous commit in history (in the log) |
| `space` / `ctrl+space` | fold or unfold the nearest / all unchanged code |
| `tab` | switch pane |
| `enter` | mark a line, then its partner: anchor; on an anchored line: remove the anchor |
| `esc` | cancel the mark |
| `/` | search in a bar at the bottom: type the text, `tab` switches between plain text and a regular expression (Go syntax), `↑` `↓` go through the earlier searches of this session, `enter` finds, `esc` cancels. Case is ignored unless the text holds a capital letter. Matches are highlighted; a match in folded code unfolds it, one beyond the pane's edge scrolls it into view |
| `.` `,` | next, previous match; past the last or first one the cursor stays. In the flow view in all files, in the log in those of the current commit: `N` `P` go to the others |
| `o` | other end of moved code |
| `m`*letter* | set a jump mark on the line of the new file |
| `'`*letter* | jump to a mark, when its file is in this diff |
| `s` | side by side or normal layout |
| `w` | show all whitespace and line ends or only changed |
| `W` | mark changed whitespace or not |
| `t` | show trailing whitespace or not |
| `e` | show all `\r\n` line ends (`␍`) or only changed |
| `r` | reflow: wrap long lines or cut them |
| `b` | bold or plain changed words |
| `f` | flow view |
| `backspace` | back to the file list, also from the flow view; a commit of one file goes back to the commits |

### Flow view

The keys of the diff view, and:

| Key | Action |
|---|---|
| `n` `p` | next, previous file bar |
| `N` `P` | next, previous commit bar in history |
| `c` | full or short commit messages |
| `C` | stay in the current commit (`N` `P` still switch) |
| `f` | single file view |

### Everywhere

| Key | Action |
|---|---|
| `\` | jump marks: `j` `k` move, `enter` jumps, `d` or `del` deletes, `esc` closes |
| `d` | a popup: show the files of `git diff ARGS` instead of what is shown, also the log: type ARGS, e.g. `--cached` or `HEAD~3 HEAD`; `↑` the current ARGS to edit, while a `git diff` is shown; `enter` show, `esc` or `enter` on nothing typed cancel. git runs in the directory jdiff was started in, also when it compares two directories or files. The popup grows with ARGS, up to the screen's width. When git fails, the popup stays and says why. |
| `R` `F5` | load again: run `git diff` or `git log` again, list the directories again or read the two files |
| `?` | help |
| `q` `ctrl+c` | quit |

## Configuration

jdiff works without any configuration.
Its files live in `$XDG_CONFIG_HOME/jdiff`, by default `~/.config/jdiff`:

| File | Content |
|---|---|
| `main.toml` | settings and the name of the theme |
| `themes/<name>.toml` | a theme |
| `state.toml` | the toggles `c` `C` `f` `s` `v` `w` as last set; written by jdiff |
| `marks.toml` | jump marks, per repository; written by jdiff |

Unknown keys and invalid values are reported in the status bar.

### main.toml

```toml
theme = "desert"                # a file in themes/; without it, delta's git config is the theme
context = 3                     # unchanged lines kept visible around changes
collapse_unchanged = true       # start with unchanged code folded
only_changed_whitespaces = true # mark only the added or removed part of whitespace
watch_interval = 1              # seconds between checks for changed files; 0 turns watching off
inotify_max_dirs = 20           # Linux: up to so many directories of changed files, the kernel reports changes; 0 turns it off
line_end = "¶"                  # drawn at the end of lines while w shows all whitespace; one column
cr_end = "␍"                    # drawn for the \r of \r\n line ends that changed, or all with e; one column
extends = "»"                   # last column of a line cut at the pane's edge (lines not wrapped); one column
precedes = "«"                  # first column of a line scrolled out to the left; one column

# The toggles, also kept in state.toml:
side_by_side = true
flow = false
preview = true
show_whitespaces = false
mark_whitespace = true
show_trailing = false
show_cr = false
wrap = false
bold_changes = true
full_messages = true
stay_in_commit = false
```

### Themes

jdiff takes its colors from delta's options: the `[delta]` section of your git config, including delta features (`delta.features`, `DELTA_FEATURES`, `--features`). Its own options are in `[jdiff]`.

Alternatively, a theme file in `themes/` sets the same options without section prefixes. Options a theme file leaves out take jdiff's defaults, not your git config. Named colors go in a `[colors]` table:

```toml
minus-style = "sand bold"
plus-emph-style = "bright-green bold"
cursor-style = "reverse"
minus-moved-style = "magenta"

[colors]
sand = "#c2b280"
```

The options:

- From delta: `minus-style`, `minus-emph-style`, `minus-non-emph-style`, `plus-style`, `plus-emph-style`, `plus-non-emph-style`, `zero-style`, `line-numbers-minus-style`, `line-numbers-plus-style`, `line-numbers-zero-style`, `line-numbers-left-style`, `line-numbers-right-style`, `file-style`, `file-decoration-style`, `inline-hint-style`, `file-added-label`, `file-removed-label`, `file-modified-label`, `file-renamed-label`.
- jdiff's own: `anchor-style`, `pending-style`, `cursor-style`, `file-bar-style`, `commit-bar-style`, `commit-hash-style`, `branch-style`, `filler-style`, `whitespace-style`, `trailing-whitespace-style`, `hidden-char-style`, `overflow-style`, `more-style` (the ↑ and ↓ of lists that go on; unset: `whitespace-style`), `search-style` (text found by `/`), `minus-indent-style`, `plus-indent-style`, `minus-moved-style`, `plus-moved-style`.

Styles use delta's syntax: attributes (`bold`, `dim`, `italic`, `ul`, `reverse`, …), then a foreground and a background color, given as a name, `bright-` name, 0–255 or `#rrggbb`. Colors beyond the 16 ANSI colors are mapped to the nearest one.

## Development

```sh
make test   # go test -race with a timeout
make vet
make vuln   # govulncheck, installed with go install golang.org/x/vuln/cmd/govulncheck@latest
make fmt
```

CI (`.github/workflows/ci.yml`) runs `fmt-check`, `vet`, `test`, `build` and `vuln` on Linux with the newest stable Go, and the tests on macOS and Windows: on pushes to any branch and every Monday, for new vulnerabilities.
When they pass, it builds jdiff for Linux (amd64, arm64), macOS (arm64, amd64) and Windows (amd64) and attaches the binaries to the run.
A tag `v*`, e.g. `git tag v1.0.0 && git push origin v1.0.0`, makes a release of them with their SHA-256 sums, which anyone can download.

To use your build in this repository, the same in its `.git/config`, with `bin/jdiff`:

```gitconfig
[diff]
	tool = jdiff
[difftool "jdiff"]
	cmd = \"$(git rev-parse --show-toplevel)/bin/jdiff\" \"$LOCAL\" \"$REMOTE\"
[difftool]
	prompt = false
[alias]
	jd = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" diff \"$@\"; }; f"
	jdiff = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" diff \"$@\"; }; f"
	jl = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
	jlog = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
```

The packages under `internal/`:

| Package | Content |
|---|---|
| `diff` | alignment (patience with Myers fallback), anchors, word-level changes, moved blocks |
| `source` | reading files and directories |
| `gitsrc` | `git log`, the files of a commit, the repository root |
| `notify` | changes to files reported by Linux (inotify) |
| `theme` | delta-compatible styles: sources (`Spec`) resolved into a `Theme` |
| `settings` | the settings |
| `config` | the configuration files, themes, state and marks |
| `ui` | the Bubble Tea views: commit list, file list, diff view, flow view, popups |
