# jdiff

An interactive diff viewer for the terminal, side by side or as one column, with manual line alignment.

jdiff matches lines on their content, ignoring indentation, so re-indented code still lines up with its original.
Where the automatic alignment guesses wrong, you pick two lines and anchor them: everything around the anchor aligns again.
It reads [delta](https://github.com/dandavison/delta)'s git config, so an existing delta theme applies right away.

## What it can do

- **Compare two files**, two directories (`git difftool --dir-diff`), or the commits of `git log`.
- **Side by side or normal layout:** two panes or one column like `git diff`; `s` switches between them.
- **Word-level highlighting** of changed lines. Indentation changes are shown character by character, so only the added or removed part is marked.
- **Anchors:** mark a line with `enter`, then its partner in the other pane. jdiff realigns around it. Anchors that would cross are offered for replacement.
- **Folding:** long unchanged runs collapse to a single row; `space` opens or closes the nearest one, `ctrl+space` all of them.
- **Moved code:** blocks moved within a file or between the files of one commit or diff are shown in magenta (moved away) and cyan (moved here), like git's `--color-moved`. `o` jumps to the other end, also into another file.
- **Flow view (`f`):** all files of a diff, or all commits of the log, one after the other in one scrollable view. Commits load as they come near the cursor.
  - `c` shows the full commit messages or only their subjects.
  - `C` keeps the cursor inside the current commit.
- **Jump marks:** `m` plus a letter marks a line of the new file, `'` plus the letter jumps back to it within the diff. `\` lists the marks; there, `enter` jumps and `d` deletes.
  - Marks are kept per repository and survive restarts.
  - A mark finds its line again by its text when the file changed.
- **Live reload:** outside the log, files on disk are watched (by default every second). When a file changes, its diff is updated, and anchors, folds and the cursor follow their lines.
- **Previews:** the commit list shows the selected commit's message, the file list a preview of the selected file's diff (`v`).
- **Whitespace:** `w` draws all whitespace as `·` and `→`, not only changed whitespace.
- **Line wrap (reflow):** `r` wraps long lines onto further lines, marked `↪`, instead of cutting them; off, `h` and `l` scroll them.
- **16-color output:** every color is an ANSI color, so your terminal palette decides how jdiff looks.

## Install

jdiff needs Go 1.24 or newer.

```sh
make build        # builds bin/jdiff
go install .      # or install it into $GOBIN
```

`jdiff --version` prints the tag or commit it was built from.

The other make targets: `make test`, `make vet`, `make fmt`, `make fmt-check` and `make clean`.

## Usage

```sh
jdiff OLD NEW                 # compare two files
jdiff DIR1 DIR2               # compare two directories
jdiff log [GIT LOG ARGS]      # browse commits, e.g. jdiff log -20 or jdiff log main..topic
jdiff --features NAMES ...    # delta features instead of delta.features, for the delta theme
```

As git's difftool, with `git jd` (like `git diff`, e.g. `git jd --cached`, `git jd main..topic`) and `git jl` (like `jdiff log`), add to `~/.gitconfig`:

```gitconfig
[diff]
	tool = jdiff
[difftool "jdiff"]
	cmd = jdiff \"$LOCAL\" \"$REMOTE\"
[difftool]
	prompt = false
[alias]
	# git difftool --dir-diff fails without changes (git 2.47.3 crashes): check first.
	jd = "!f() { if git diff --quiet \"$@\"; then echo \"no changes\"; else git difftool --dir-diff \"$@\"; fi; }; f"
	jdiff = "!f() { if git diff --quiet \"$@\"; then echo \"no changes\"; else git difftool --dir-diff \"$@\"; fi; }; f"
	jl = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
	jlog = "!f() { cd \"${GIT_PREFIX:-.}\" && jdiff log \"$@\"; }; f"
```

Against the working tree, git links your working copy into the comparison, so edits you save while jdiff is open show up live.

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
| `N` `P` | next, previous commit (in the log) |
| `f` | flow view of all files |
| `v` | show or hide the diff preview |
| `s` | side by side or normal preview |
| `w` | show all whitespace or only changed |
| `r` | reflow: wrap long lines or cut them |
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
| `N` `P` | next, previous commit (in the log) |
| `space` / `ctrl+space` | fold or unfold the nearest / all unchanged code |
| `tab` | switch pane |
| `enter` | mark a line, then its partner: anchor; on an anchored line: remove the anchor |
| `esc` | cancel the mark |
| `o` | other end of moved code |
| `m`*letter* | set a jump mark on the line of the new file |
| `'`*letter* | jump to a mark, when its file is in this diff |
| `s` | side by side or normal layout |
| `w` | show all whitespace or only changed |
| `r` | reflow: wrap long lines or cut them |
| `f` | flow view |
| `backspace` | back to the list |

### Flow view

The keys of the diff view, and:

| Key | Action |
|---|---|
| `n` `p` | next, previous file bar |
| `N` `P` | next, previous commit bar |
| `c` | full or short commit messages |
| `C` | stay in the current commit (`N` `P` still switch) |
| `f` | single file view |

### Everywhere

| Key | Action |
|---|---|
| `\` | jump marks: `j` `k` move, `enter` jumps, `d` or `del` deletes, `esc` closes |
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
only_changed_whitespaces = true # compare indentation character by character
watch_interval = 1              # seconds between checks for changed files; 0 turns it off

# The toggles, also kept in state.toml:
side_by_side = true
flow = false
preview = true
show_whitespaces = false
wrap = false
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
- jdiff's own: `anchor-style`, `pending-style`, `cursor-style`, `file-bar-style`, `commit-bar-style`, `commit-hash-style`, `branch-style`, `filler-style`, `whitespace-style`, `minus-indent-style`, `plus-indent-style`, `minus-moved-style`, `plus-moved-style`.

Styles use delta's syntax: attributes (`bold`, `dim`, `italic`, `ul`, `reverse`, …), then a foreground and a background color, given as a name, `bright-` name, 0–255 or `#rrggbb`. Colors beyond the 16 ANSI colors are mapped to the nearest one.

## Development

```sh
make test   # go test -race with a timeout
make vet
make fmt
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
	jd = "!f() { if git diff --quiet \"$@\"; then echo \"no changes\"; else git difftool --dir-diff \"$@\"; fi; }; f"
	jdiff = "!f() { if git diff --quiet \"$@\"; then echo \"no changes\"; else git difftool --dir-diff \"$@\"; fi; }; f"
	jl = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
	jlog = "!f() { top=$(git rev-parse --show-toplevel); cd \"${GIT_PREFIX:-.}\" && \"$top/bin/jdiff\" log \"$@\"; }; f"
```

The packages under `internal/`:

| Package | Content |
|---|---|
| `diff` | alignment (patience with Myers fallback), anchors, word-level changes, moved blocks |
| `source` | reading files and directories |
| `gitsrc` | `git log`, the files of a commit, the repository root |
| `theme` | delta-compatible styles: sources (`Spec`) resolved into a `Theme` |
| `settings` | the settings |
| `config` | the configuration files, themes, state and marks |
| `ui` | the Bubble Tea views: commit list, file list, diff view, flow view, popups |
