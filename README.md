# jdiff
[![coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/ObiWahn/jdiff/badges/coverage.json)](https://obiwahn.github.io/jdiff/coverage.html)

An interactive diff viewer for the terminal, side by side or as one column, with manual line alignment.

jdiff matches lines on their content, ignoring indentation, so re-indented code still lines up with its original.
Where the automatic alignment guesses wrong, you pick two lines and anchor them: everything around the anchor aligns again.
It reads [delta](https://github.com/dandavison/delta)'s git config, so an existing delta theme applies right away.

## Inspired by delta

jdiff is inspired by [delta](https://github.com/dandavison/delta), the syntax-highlighting pager for git written in Rust.
delta formats the output of git once and hands it to a pager; jdiff is a program you work in, so it can do what a pager cannot:

- **Resizing:** the diff is laid out again when the terminal changes size; delta lays it out once, for the width it started with.
- **Switching the layout:** two panes or one column like `git diff`; `s` switches between them while you look at the diff. In delta, side by side is chosen at startup.
- **Wrap:** `w` switches between wrapping long lines onto further lines, marked `↪`, and cutting them; delta wraps a line as often as `--wrap-max-lines` allows, set at startup, and truncates the rest.
  - A cut line that goes on past its pane ends in `»`, one scrolled out to the left (`h` `l`) starts with `«`.
- **More or less context:** long unchanged runs collapse to a single row; `space` opens or closes the nearest one, `ctrl+space` all of them. delta shows the context lines git gave it.
- **Walking the history:** `jdiff log` lists the commits, opens a commit's files one by one, and `N` `P` step to the next or previous commit, keeping the open file.
- **Following moved code:** like delta, jdiff colors blocks moved within a file or between the files of one commit or diff, magenta where they moved away and cyan where they moved to, as git's `--color-moved` does. `o` jumps to the other end, also into another file.
- **Auto reload:** outside the log, files on disk are watched: on Linux the kernel reports their changes (inotify), elsewhere, or when the files are in more than `inotify_max_dirs` directories, they are checked every second. When you save an edit, its diff updates while jdiff is open, and anchors, folds and the cursor follow their lines. `r`, `F5` or `ctrl+r` runs `git diff` or `git log` again, or lists the directories again, so new files and commits show up too; `R` does so by itself whenever the repository changes, see [Keys](#keys). delta shows the diff as it was when git ran.
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
- **Whitespace:** changed indentation, tabs and runs of spaces are marked as `·` and `→`, only their added or removed part, not single spaces between changed words; `W` draws all whitespace so and line ends as `¶` (`line_end`), `ctrl+w` turns the marking of changed whitespace off, `t` shows trailing whitespace. Changed `\r\n` line ends show as `␍`, all of them with `e`. Invisible characters, among them those that change the text direction, show as `☠`.
- **16-color output:** every color is an ANSI color, so your terminal palette decides how jdiff looks.

## Install

On macOS (Apple Silicon or Intel), install the upstream Homebrew cask:

```sh
brew tap ObiWahn/jdiff https://github.com/ObiWahn/jdiff.git
brew install --cask obiwahn/jdiff/jdiff
```

To update it, run `brew update && brew upgrade --cask obiwahn/jdiff/jdiff`.
The cask installs a prebuilt binary; Go is not needed.

On Linux, download the package for your distribution from the [upstream releases](https://github.com/ObiWahn/jdiff/releases) and install it; it pulls in git:

```sh
sudo apt install ./jdiff_<version>_amd64.deb               # Debian, Ubuntu
sudo dnf install ./jdiff-<version>-1.x86_64.rpm             # Fedora, RHEL
sudo apk add --allow-untrusted ./jdiff_<version>_x86_64.apk # Alpine
sudo pacman -U ./jdiff-<version>-1-x86_64.pkg.tar.zst       # Arch
```

On Windows, install it with Scoop:

```sh
scoop bucket add jdiff https://github.com/ObiWahn/jdiff
scoop install jdiff
```

Or download an archive or a bare binary from the [upstream releases](https://github.com/ObiWahn/jdiff/releases), for Linux, macOS or Windows.
Extract the archive, or rename the binary, and put `jdiff` (or `jdiff.exe`) on your `PATH`.
On Linux and macOS, make a bare binary executable with `chmod +x`.

To build from source, jdiff needs Go 1.24 or newer:

```sh
make build        # builds bin/jdiff
go install .      # or install it into $GOBIN
```

`jdiff --version` prints the tag or commit it was built from, `jdiff --license` the license and the dependencies linked into it, `jdiff --readme` this README without its badge.

The other make targets: `make test`, `make test-race`, `make cover`, `make tag`, `make vet`, `make vuln` (govulncheck), `make fmt`, `make fmt-check` and `make clean`.

## Usage

```sh
jdiff OLD NEW                 # compare two files
jdiff DIR1 DIR2               # compare two directories
jdiff log [GIT LOG ARGS]      # browse commits, e.g. jdiff log -20 or jdiff log main..topic
jdiff diff [GIT DIFF ARGS]    # what git diff shows: jdiff diff (unstaged), jdiff diff --cached (the index), jdiff diff HEAD; u adds the untracked files
jdiff --features NAMES ...    # delta features instead of delta.features, for the delta theme
```

Git aliases, in `~/.gitconfig`: `git jd` takes the arguments of `git diff` (`git jd --cached`, `git jd main..topic`), `git jl` those of `git log`:

```gitconfig
[alias]
	jd = "!f() { : git diff; cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jdiff = "!f() { : git diff; cd \"${GIT_PREFIX:-.}\" && jdiff diff \"$@\"; }; f"
	jl = "!f() { : git log; cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
	jlog = "!f() { : git log; cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
```

The null command `: git diff;` does nothing when the alias runs; git's bash completion reads it
and completes `git jd` like `git diff`: options, branches and paths. It has to come first in the
function, before any other command.

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
| `N` `P`, `n` `p` | next, previous commit in history, also with `--reverse` |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | page up, down |
| `g` `G` | first, last commit |
| `enter` | show the files of the commit |
| `f` | flow view of all commits |

### File list

| Key | Action |
|---|---|
| `j` `k`, `n` `p` | move |
| `pgup` `pgdn` (`ctrl+b` `ctrl+f`) | scroll the diff preview; page up, down while it is hidden |
| `g` `G` | first, last file |
| `enter` | show the diff of the file |
| `'`*letter* | open the file of a jump mark |
| `N` `P` | next, previous commit in history (in the log) |
| `f` | flow view of all files |
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
| `/` | search in a bar at the bottom: type the text; `tab` switches between changed lines (added, removed and moved; the default) and all lines, `shift+tab` between plain text and a regular expression (Go syntax), both kept in `state.toml`; `↑` `↓` go through the earlier searches of this session, `enter` finds, `esc` or `backspace` on nothing typed cancels. Case is ignored unless the text holds a capital letter. Matches are highlighted; a match in folded code unfolds it, one beyond the pane's edge scrolls it into view |
| `.` `,` | next, previous match; past the last or first one the cursor stays. In the flow view in all files, in the log in those of the current commit: `N` `P` go to the others |
| `o` | other end of moved code |
| `m`*letter* | set a jump mark on the line of the new file |
| `'`*letter* | jump to a mark, when its file is in this diff |
| `f` | flow view |
| `backspace` | back to the file list, also from the flow view; a commit of one file goes back to the commits |

### Flow view

The keys of the diff view, and:

| Key | Action |
|---|---|
| `n` `p` | next, previous file bar |
| `N` `P` | next, previous commit bar in history |
| `f` | single file view |
| `c` | full or short commit messages, in the log |
| `C` | stay in the current commit, in the log (`N` `P` still switch) |

### Everywhere

| Key | Action |
|---|---|
| `\` | jump marks: `j` `k` move, `enter` jumps, `d` or `del` deletes, `esc` closes |
| `d` | a popup: show the files of `git diff ARGS` instead of what is shown, also the log: type ARGS, e.g. `--cached` or `HEAD~3 HEAD`; `↑` the current ARGS to edit, while a `git diff` is shown; `enter` show, `esc`, or `enter` or `backspace` on nothing typed cancel. git runs in the directory jdiff was started in, also when it compares two directories or files. The popup grows with ARGS, up to the screen's width. When git fails, the popup stays and says why. |
| `s` | side by side or normal layout, also of the preview |
| `v` | show or hide the message box or diff preview |
| `W` | show all whitespace and line ends or only changed |
| `ctrl+w` | mark changed whitespace or not |
| `t` | show trailing whitespace or not |
| `e` | show all `\r\n` line ends (`␍`) or only changed |
| `b` | bold or plain changed words |
| `w` | wrap long lines or cut them |
| `u` | in `jdiff diff`, also show the untracked files that are not ignored, as added, and list the files anew; kept in `state.toml`, so later runs show them too. They show where `git diff` compares with the working tree: `jdiff diff`, `jdiff diff HEAD`, not `--cached` or two commits. Your index stays as it is: jdiff marks them in a temporary copy of it, and the header says `+ untracked`. Directories, as nested repositories, and files that cannot be read, as broken symlinks, are left out. |
| `S` `F8` | a popup with all these settings, also `c` and `C` of the flow view, to find them, and, only there, the minutes until auto reload ends, typed in a line after `enter` on them: `j` `k` move, `enter`, `space` or the letter toggles, kept in `state.toml`; the popup stays open, `S`, `F8`, `esc` or `q` close it |
| `r` `F5` `ctrl+r` | load again: run `git diff` or `git log` again, list the directories again or read the two files |
| `R` | auto reload: load again whenever `git status` changes, as with a commit, `git add`, a checkout, a new file or an edit of a file not yet in the diff; the status bar says `auto reload` while it is on. It runs `git status` every `auto_reload_interval` (10 seconds) and, on Linux, at once when the kernel reports a change of `HEAD`, the index or the refs of the current branch; so new files and edits of files not yet in the diff take up to that long. After the minutes set in the settings (5) without a key pressed or a change it ends, and with it its git commands. Not saved: off at each start |
| `?` | help |
| `q` `ctrl+c` | quit |

## Configuration

jdiff works without any configuration.
Its files live in `$XDG_CONFIG_HOME/jdiff`, by default `~/.config/jdiff`:

| File | Content |
|---|---|
| `main.toml` | settings and the name of the theme |
| `themes/<name>.toml` | a theme |
| `state.toml` | the toggles `b` `c` `C` `e` `f` `s` `t` `u` `v` `w` `W` `ctrl+w`, the minutes of auto reload, and how `/` searches, as last set; written by jdiff |
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
auto_reload_interval = 10       # seconds between runs of git status while auto reload (R) is on
line_end = "¶"                  # drawn at the end of lines while W shows all whitespace; one column
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
search_regex = false            # / searches for a regular expression (shift+tab in its bar)
search_all = false              # / searches all lines, not only changed ones (tab)
untracked = false               # jdiff diff also shows untracked files (u)
auto_reload_timeout = 5         # minutes without keys or changes until auto reload (R) ends; 0 never
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
make test        # go test -race with a timeout; the sweeps (tests named ...Sweep) without -race
make test-race   # all tests with -race, the sweeps too: before a release
make cover       # test coverage: by package and function in bin/coverage.txt, lines in bin/coverage.html
make tag         # on a clean master, after test-race: tag HEAD with the next vX.Y.Z, not pushed
make vet
make vuln   # govulncheck, installed with go install golang.org/x/vuln/cmd/govulncheck@latest
make fmt
```

CI (`.github/workflows/ci.yml`) runs `fmt-check`, `vet`, `test`, `cover`, `build` and `vuln` on Linux with the newest stable Go, and the tests on macOS and Windows: on pushes to any branch and every Monday, for new vulnerabilities.
A run's summary shows the total test coverage, its artifact `coverage` the report by package and function and the covered lines; pushes to upstream's `master` update the total for the badge and both reports, in the branch `badges`, which GitHub Pages serves: [lines](https://obiwahn.github.io/jdiff/coverage.html), [by package and function](https://obiwahn.github.io/jdiff/coverage.txt). A tag `v*` keeps its reports there for good, in a directory named after it, linked from its release. GitLab CI (`.gitlab-ci.yml`) reports the coverage too.
When they pass, [GoReleaser](https://goreleaser.com/) builds jdiff for Linux (amd64, arm64), macOS (arm64, amd64) and Windows (amd64).
Branches and scheduled runs (including on forks) build snapshots and attach the archives, binaries, packages, SHA-256 sums and generated cask to the run without publishing.
A tag `v*` pushed to `ObiWahn/jdiff`, e.g. `git tag v0.0.3 && git push upstream v0.0.3`, publishes the archives, binaries, packages and `sha256sums.txt` as a GitHub release, then updates `Casks/jdiff.rb` on upstream's `master` branch.
Prereleases do not update the cask.

The upstream repository itself is the Homebrew tap: the explicit URL in `brew tap` avoids needing a separate `homebrew-jdiff` repository.
The release job uses its `GITHUB_TOKEN` with `contents: write`; no extra token or tap repository is needed.
If branch protection is enabled, it must allow the release bot to update `Casks/jdiff.rb` on `master`.
The macOS binaries are not notarized; the cask removes the quarantine attribute only from the staged jdiff binary so it can run.

To check the release setup locally with GoReleaser 2.19.0:

```sh
goreleaser check
goreleaser release --snapshot --clean # builds all targets and the cask, publishes nothing
```

To use your build in this repository, the same in its `.git/config`, with `bin/jdiff`:

```gitconfig
[diff]
	tool = jdiff
[difftool "jdiff"]
	cmd = \"$(git rev-parse --show-toplevel)/bin/jdiff\" \"$LOCAL\" \"$REMOTE\"
[difftool]
	prompt = false
[alias]
	jd = "!f() { : git diff; top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" diff \"$@\"; }; f"
	jdiff = "!f() { : git diff; top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" diff \"$@\"; }; f"
	jl = "!f() { : git log; top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
	jlog = "!f() { : git log; top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
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
