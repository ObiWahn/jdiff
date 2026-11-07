# jdiff: rules for working on it

## Before writing code

- Look for related helpers and code first, and use or extend them instead of writing the same
  thing again: e.g. `lineInput` (with `cancels`), `inputBox`, `box`, `popupTitle`,
  `overlay`/`overlayAt`, `windowAt`/`windowAround`, `statusText`/`statusLine`,
  `toggle`/`saveState`, `printable`, `cutLeft`.
- Share code where a substantial amount of it is the same, or where the copies must behave the
  same and should not drift apart: a fix in one would be needed in the other. A few lines equal
  by coincidence stay separate, even if identical today.
- Saving a few lines is no reason. Against a helper count: bool or -1 parameters, closures in place
  of plain loops, long argument lists, and coupling views that may diverge. Within one type it
  costs less than across views.
- Comments on a shared helper say what its callers rely on and who deliberately does not use
  it, not where it is used.

## Colors

- Every color and style of the UI comes from the theme (`internal/theme`), never from a color or
  style written into `internal/ui`.
- A new color is a new theme option:
  1. a field in `theme.Spec` (`spec.go`) with its `toml` key and `git:"jdiff"` for jdiff's own
     options, and its default in `Builtin`;
  2. a field in `theme.Theme` (`theme.go`), resolved in `Resolve`: with `style` and its default,
     or with `styleOrStyle` falling back to an existing style;
  3. a test in `theme_test.go`: the default, and the option set;
  4. the option in the list of theme options in `doc/config.md`.
- After a change of colors, check that `internal/ui` sets none itself: no `lipgloss.Color`,
  `Foreground`, `Background` or escape sequences outside `internal/theme`.

## Keys and status bars

- The status bars are short and come from `statusKeys`, in one order on all pages:
  enter · backspace · N/P · f · s · d · o · ? · q;
  each page shows only those that work there, the other keys are in the help (?). The labels of
  f and s name what the key switches to, so pressing it changes only its own label. The anchors
  stand at the right end.
- The table of keys under `### Everywhere` in `doc/keybindings.md` decides the order of the
  settings: the popup and the help's block of settings follow it at startup (`ui.OrderSettings`).
  Change the order there, not in `settingOptions` or `bindings`. A setting with a key of its own is
  a row of that table, unless it works only in one view (`logFlow`); `TestKeybindingsOrder` checks
  that. The status bars keep their own order, above.
- The direction in history (N n next, P p previous, also with `--reverse`) is decided in one
  place only: `historyDir` and `historyOrder.step` in `internal/ui/messages.go`.
- Toggles are fields of `settings.Toggles`, kept in `state.toml`; list a new one in the help's
  line of saved toggles and in the example of `main.toml` in `doc/config.md`.

## Tests

- Test views with numbered items or lines (`f00` …, `c00` …, `x0` …), so that counts and
  positions can be checked exactly.
- Sweep window sizes where edge cases hide: every terminal height from the smallest drawn
  (`minHeight`) up, and around "all fits". Name the sizes that must stay in a comment, so that a
  sweep made faster keeps them; prefer a narrow terminal or `t.Parallel()` over fewer sizes.
- `make test` runs the tests with `-race`, then the sweeps, tests named `…Sweep`, without it; each
  run has a 15s timeout, keep both under that. A sweep draws thousands of frames in one goroutine:
  the race detector can find nothing there, but makes drawing about ten times slower. A test that
  runs goroutines (watchers, commands) or shares state between parallel tests (the `compiled`
  matcher of the search) is no sweep and keeps `-race`; run each size of a sweep as a parallel
  subtest. Before a release, `make test-race` runs all tests with `-race`, the sweeps too, with a
  40s timeout; `make tag` runs it before tagging.
- Why: since Bubble Tea and Lip Gloss v2 the tests draw with colors, as the program does (v1 drew
  them plain without a terminal), and `x/ansi` 0.11 measures widths by grapheme clusters. With
  `-race` the UI package took 19s, against about 10s before; without it 1.5s. Drawing a full
  colored 200x60 diff went from 6.3ms to 7.6ms, no difference in use.

- A refactoring changes no output: record the snapshots of `../jdiff-snap` (`snap write`) on a
  clean commit first, `snap check` after each step; explain every frame that differs.

## Building and trying

- Run `make build` after every change, also of tests or docs: `~/.bin/jdiff` links to
  `bin/jdiff`.
- Try the TUI in a tmux on a private socket (`tmux -L <name>`), never the default server, and
  start jdiff with `XDG_CONFIG_HOME` set to a scratch directory, so that toggles pressed while
  trying do not reach the user's `~/.config/jdiff/state.toml`.
