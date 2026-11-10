# Development

## Make targets

```sh
make build       # builds bin/jdiff
make test        # go test -race with a timeout; the sweeps (tests named ...Sweep) without -race
make test-race   # all tests with -race, the sweeps too: before a release
make cover       # test coverage: by package and function in bin/coverage.txt, lines in bin/coverage.html
make licenses    # writes LICENSE-THIRD-PARTY after a change of dependencies; make test fails until then
make tag         # on a clean master, after test-race: tag HEAD with the next vX.Y.Z, not pushed
make vet
make vuln        # govulncheck, installed with go install golang.org/x/vuln/cmd/govulncheck@latest
make fmt
make fmt-check
make clean
```

## CI

CI (`.github/workflows/ci.yml`) runs `fmt-check`, `vet`, `test`, `cover`, `build` and `vuln` on Linux with the newest stable Go, and the tests on macOS and Windows: on pushes to any branch and every Monday, for new vulnerabilities.

A run's summary shows the total test coverage, its artifact `coverage` the report by package and function and the covered lines.
Pushes to upstream's `master` update the total for the badge and both reports, in the branch `badges`, which GitHub Pages serves: [lines](https://obiwahn.github.io/jdiff/coverage.html), [by package and function](https://obiwahn.github.io/jdiff/coverage.txt).
A tag `v*` keeps its reports there for good, in a directory named after it, linked from its release.
GitLab CI (`.gitlab-ci.yml`) reports the coverage too.

## Releases

When CI passes, [GoReleaser](https://goreleaser.com/) builds jdiff for Linux (amd64, arm64), macOS (arm64, amd64) and Windows (amd64).
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

## Using your build in this repository

The [difftool and aliases](usage.md) in this repository's `.git/config`, with `bin/jdiff`:

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

## Trying auto reload

`scripts/watch-demo [seconds [files [dir]]]` creates and removes small files for a while, 60 seconds and at most 5 at once by default, in a new directory `watch-demo-<pid>` that it removes at the end, also on `ctrl+c`.
Start `jdiff diff` in the repository, turn on `R` and `u` (the untracked files), then run it there.

## Packages

The packages under `internal/`, and `watch` beside them, which uses nothing of jdiff:

| Package | Content |
|---|---|
| `diff` | alignment (patience with Myers fallback), anchors, word-level changes, moved blocks |
| `source` | reading files and directories |
| `gitsrc` | `git log`, the files of a commit, the repository root |
| `watch` | changes to files and directories as the system reports them, the most recently used directories up to a limit; a backend per system, Linux (inotify) only yet |
| `theme` | delta-compatible styles: sources (`Spec`) resolved into a `Theme` |
| `settings` | the settings |
| `config` | the configuration files, themes, state and marks |
| `ui` | the Bubble Tea views: commit list, file list, diff view, flow view, popups |

`internal/keys/keybindings.toml` binds the keys of all pages to actions, and holds the words of the status bar, the help and the settings popup, and the order of the settings.

## Kept on purpose

Things that look like a mistake or a duplicate but are not; read this before "fixing" them.

### watch is not under internal/

The package `watch` stands at the top level, beside `internal/`, so that other programs can
import it: it uses nothing of jdiff and is meant to be used elsewhere. Keep it free of jdiff's
packages, and do not move it under `internal/`.

### The keys of the flow view and the diff view are shared only in part

`Flow.key` and `FileDiff.key` handle nearly the same actions. Those that must work alike, the
motions, quit, pane, half_page, scroll, change, search, match, set_mark and jump, are handled once,
by `diffKey` (`messages.go`) on the interface `diffKeyView`. The rest stays in each view's own
`switch`, as it differs: the flow view holds all files and commits and moves between them inside
itself (`next_file`, `history`, `to_files`), the diff view of one file asks the view above it;
anchors and folds go to the file under the cursor (`delegate`) or the one file (`act`); and the
marks take different arguments. Do not move those into `diffKey`.

### inotify_max_dirs is applied twice to the shared watcher

The views share one watcher, owned by `App` (`app.go`). A change of `inotify_max_dirs` in the
settings popup reaches it twice: `Diff` and `FileDiff` call `diskWatch.limit` when they handle
`settingChangedMsg`, and `App.Update` applies it again at its end if it differs from `App.limit`.
Both are needed:

- `SettingsPopup` is one of the wrappers inside `App.Model`: it changes the setting and calls the
  view's `Update` with `settingChangedMsg` directly, so the message never passes through `App`,
  and `App` cannot apply the limit before the view sees it. The view polls right after, and that
  poll has to see what is watched with the new limit.
- Views without files on disk (`Log`, `Flow`) have no `diskWatch`; for them only `App`'s check
  applies it.

The second call changes no watch: with the limit unchanged, `Watcher.apply` finds the directories
it keeps already watched. Moving it into one place would mean routing the popup's messages through
`App` or giving every view a `diskWatch`.
