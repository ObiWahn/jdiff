# Configuration

jdiff works without any configuration.
Its files live in `$XDG_CONFIG_HOME/jdiff`, by default `~/.config/jdiff`:

| File | Content |
|---|---|
| `main.toml` | settings and the name of the theme |
| `themes/<name>.toml` | a theme |
| `state.toml` | the toggles `s` `f` `u` `i` `I` `w` `W` `ctrl+w` `t` `e` `b` `v` `c` `C` `R`, the minutes of auto reload, the directories watched, the paths `ignored_hide` hides, and how `/` searches, as last set; written by jdiff |
| `marks.toml` | jump marks, per repository; written by jdiff |
| `keybindings.toml` | [your keys](#keybindingstoml), where they differ from the built-in ones |

Unknown keys and invalid values are reported in the status bar.

## main.toml

```toml
theme = "desert"                # a file in themes/; without it, delta's git config is the theme
context = 3                     # unchanged lines kept visible around changes
collapse_unchanged = true       # start with unchanged code folded
only_changed_whitespaces = true # mark only the added or removed part of whitespace
watch_interval = 1              # while auto reload (R) is on: seconds between checks of files not watched, 0.1 to 86400; 0 neither checks nor watches
auto_reload_interval = 10       # seconds between runs of git status while auto reload (R) is on, up to 86400
# While R is on, a change of a path from the top of the working tree that one of these regular
# expressions matches runs no git status, and a directory that one matches (with a / at the end:
# '^vendor/$') is not watched, nor those below it; [] for none. By default editors' files.
watch_ignore = ['\.sw[a-px]$', '~$', '(^|/)\.#', '(^|/)#[^/]*#$', '(^|/)4913$', '\.tmp$']
line_end = "¶"                  # drawn at the end of lines while W shows all whitespace; one column
cr_end = "␍"                    # drawn for the \r of \r\n line ends that changed, or all with e; one column
extends = "»"                   # last column of a line cut at the pane's edge (lines not wrapped); one column
precedes = "«"                  # first column of a line scrolled out to the left; one column

# The toggles, also kept in state.toml:
side_by_side = true
flow = false
untracked = false               # jdiff diff also shows untracked files (u)
ignored = false                 # with them, those .gitignore ignores (i)
excluded = false                # with them, those .git/info/exclude ignores (I)
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
auto_reload = false             # start with auto reload (R) on; -R only for a run
auto_reload_timeout = 15        # minutes without keys or changes until auto reload (R) ends; 0 never
inotify_max_dirs = 100          # while R is on, Linux: the most directories watched, the most recently used, besides HEAD and the index; 0 those only
# Ignored and excluded files (i, I) that one of these regular expressions matches are not shown,
# by their path from the top of the working tree, nor those below a directory one matches (with a /
# at the end). One that is no regular expression is left out with a warning; [] hides none. By
# default directories of tools. The settings popup (o) edits them; state.toml gets one per line, as here.
ignored_hide = [
    '(^|/)node_modules/$',
    '(^|/)bower_components/$',
    '(^|/)\.venv/$',
    '(^|/)__pycache__/$',
    '(^|/)\.mypy_cache/$',
    '(^|/)\.pytest_cache/$',
    '(^|/)\.ruff_cache/$',
    '(^|/)\.tox/$',
    '(^|/)\.gradle/$',
    '(^|/)\.terraform/$',
    '(^|/)\.next/$',
    '(^|/)\.nuxt/$',
    '(^|/)\.cache/$',
    '(^|/)\.direnv/$',
]
```

## keybindings.toml

Changes keys; without it, jdiff uses its built-in keys. List only what you change, by page and action:

```toml
commit_list = [
    { action = "move", keys = ["x", "y"] },
]
everywhere = [
    { action = "settings", keys = ["O"] },
]
```

- The pages are `everywhere`, `diff_view`, `flow_view` (keys of the diff view apply too), `commit_list`, `file_list` and `doc_view` (the README of `--readme`); the actions and their keys are those of the [built-in file](../internal/keys/keybindings.toml).
- The rows of an action replace all its rows on that page: above, `x` `y` take the place of both `j` `k` and `down` `up` in the commit list. To keep a key, list its row too.
- An action that goes two ways takes two keys, in the order of the built-in row: for `move` down, then up.
- The status bar, the help and the settings popup show your keys with their usual words. Messages, and the keys inside popups and input fields, stay as built in.
- A file with a problem, such as an unknown page, action or field, a row without keys, or a key with two actions on one page, changes no key: the status bar says what is wrong.

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

- From delta: `minus-style`, `minus-emph-style`, `minus-non-emph-style` (unset: `minus-style`), `plus-style`, `plus-emph-style`, `plus-non-emph-style` (unset: `plus-style`), `zero-style`, `line-numbers-minus-style`, `line-numbers-plus-style`, `line-numbers-zero-style`, `line-numbers-left-style`, `line-numbers-right-style`, `file-style`, `file-decoration-style`, `inline-hint-style`, `file-added-label`, `file-removed-label`, `file-modified-label`, `file-renamed-label`.
- jdiff's own: `anchor-style`, `pending-style`, `cursor-style`, `file-bar-style`, `commit-bar-style`, `commit-hash-style`, `branch-style`, `filler-style` and `whitespace-style` (unset: the color of `file-decoration-style`), `added-file-style` (the `U`, `I`, `X` of untracked, ignored and excluded files; unset: `filler-style`), `trailing-whitespace-style`, `hidden-char-style`, `overflow-style`, `more-style` (the ↑ and ↓ of lists that go on; unset: `whitespace-style`), `search-style` (text found by `/`), `minus-indent-style` (unset: `minus-emph-style`), `plus-indent-style` (unset: `plus-emph-style`), `minus-moved-style`, `plus-moved-style`, `doc-heading-style` (headings of `--readme`; unset: `branch-style`), `doc-code-style` (code there; unset: `file-style`), `doc-link-style` (links there), `heading-style` (headings of lists and boxes and the active pane's title; bold), `inactive-heading-style` (the inactive pane's title, side by side; dim), `key-style` (the keys in the help; bold).

Styles use delta's syntax: attributes (`bold`, `dim`, `italic`, `ul`, `reverse`, …), then a foreground and a background color, given as a name, `bright-` name, 0–255 or `#rrggbb`. Colors beyond the 16 ANSI colors are mapped to the nearest one.
