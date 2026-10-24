# jdiff: rules for working on it

## Before writing code

- Look for related helpers and code first, and use or extend them instead of writing the same
  thing again: e.g. `lineInput`, `box`, `overlay`/`overlayAt`, `windowAt`/`windowAround`,
  `statusText`/`statusLine`, `toggle`/`saveState`, `printable`, `cutLeft`.
- Where two places do the same, factor it into one helper when that is not too costly; an
  abstraction that makes the callers harder to read, or covers only a coincidence, is not worth
  it.

## Colors

- Every color and style of the UI comes from the theme (`internal/theme`), never from a color or
  style written into `internal/ui`.
- A new color is a new theme option:
  1. a field in `theme.Spec` (`spec.go`) with its `toml` key and `git:"jdiff"` for jdiff's own
     options, and its default in `Builtin`;
  2. a field in `theme.Theme` (`theme.go`), resolved in `Resolve`: with `style` and its default,
     or with `styleOrStyle` falling back to an existing style;
  3. a test in `theme_test.go`: the default, and the option set;
  4. the option in the README's list of theme options.
- After a change of colors, check that `internal/ui` sets none itself: no `lipgloss.Color`,
  `Foreground`, `Background` or escape sequences outside `internal/theme`.

## Keys and status bars

- The status bars list their keys in one order on all pages:
  enter · backspace · space · ]/[ · / · n/p · N/P · f · v · d · ? · q;
  each page shows only the keys that work there. The anchors stand at the right end.
- The direction in history (N n next, P p previous, also with `--reverse`) is decided in one
  place only: `historyDir` and `historyOrder.step` in `internal/ui/messages.go`.
- Toggles are fields of `settings.Toggles`, kept in `state.toml`; list a new one in the help's
  line of saved toggles and in the README's example of `state.toml`.

## Tests

- Test views with numbered items or lines (`f00` …, `c00` …, `x0` …), so that counts and
  positions can be checked exactly.
- Sweep window sizes where edge cases hide: every terminal height from the smallest drawn
  (`minHeight`) up, and around "all fits". Name the sizes that must stay in a comment, so that a
  sweep made faster keeps them; prefer a narrow terminal or `t.Parallel()` over fewer sizes.
- `make test` runs with `-race` and a 15s timeout; keep it under that.

## Building and trying

- Run `make build` after every change, also of tests or docs: `~/.bin/jdiff` links to
  `bin/jdiff`.
- Try the TUI in a tmux on a private socket (`tmux -L <name>`), never the default server, and
  start jdiff with `XDG_CONFIG_HOME` set to a scratch directory, so that toggles pressed while
  trying do not reach the user's `~/.config/jdiff/state.toml`.
