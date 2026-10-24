# TODO

## Confirm the macOS and Windows test fixes in CI

The first CI runs on macOS and Windows failed in four tests, all in the
tests, not in jdiff. They are fixed without a machine to try them on; the
next CI run on macOS and Windows confirms them:

- `gitsrc` `TestDiff` (macOS, Windows): the repository's directory came from
  `t.TempDir()`, git reports it resolved: `/private/var/…` for `/var/…` on
  macOS, likely long names for 8.3 short ones such as `RUNNER~1` on Windows.
  The test repositories now live in `filepath.EvalSymlinks(t.TempDir())`.
- `gitsrc` `TestTopLevel` (Windows): git writes `C:/…`. `TopLevel` now
  returns the path in the system's form (`filepath.Clean`), so that it
  compares with other paths, e.g. for jump marks.
- `config` `TestDir` (Windows): `/xdg` is not absolute there and
  `os.UserHomeDir` reads `USERPROFILE`. The test uses temporary directories
  and sets `USERPROFILE` too.
- `gitsrc` `TestCatFilesErrorStopsGit` (Windows): the fake git was a bash
  script, which Windows does not run as `git`. It is a small Go program now,
  built by the test.

If one still fails, skip it there with a pointer to this file and note why.
