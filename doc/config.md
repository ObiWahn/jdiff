# Configuration

jdiff works without any configuration.
Its files live in `$XDG_CONFIG_HOME/jdiff`, by default `~/.config/jdiff`:

| File | Content |
|---|---|
| `main.toml` | settings and the name of the theme |
| `themes/<name>.toml` | a theme |
| `state.toml` | the toggles `s` `f` `u` `w` `W` `ctrl+w` `t` `e` `b` `v` `c` `C`, the minutes of auto reload, and how `/` searches, as last set; written by jdiff |
| `marks.toml` | jump marks, per repository; written by jdiff |

Unknown keys and invalid values are reported in the status bar.

## main.toml

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
untracked = false               # jdiff diff also shows untracked files (u)
wrap = false
show_whitespaces = false
mark_whitespace = true
show_trailing = false
show_cr = false
bold_changes = true
preview = true
full_messages = true
stay_in_commit = false
search_regex = false            # / searches for a regular expression (shift+tab in its bar)
search_all = false              # / searches all lines, not only changed ones (tab)
auto_reload_timeout = 5         # minutes without keys or changes until auto reload (R) ends; 0 never
```

## Themes

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
