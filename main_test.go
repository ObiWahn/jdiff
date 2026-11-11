package main

import (
	"errors"
	"flag"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/ui"
)

// runMainEnv makes the test binary run main instead of the tests, for
// runMain, as what ends in os.Exit cannot run in the tests' process.
const runMainEnv = "JDIFF_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) != "" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// noRaceSleep keeps the race detector from waiting a second at the end of
// each program the tests run, which it does to report late races.
const noRaceSleep = "GORACE=atexit_sleep_ms=0"

// runMain runs main with args in dir, without the user's configuration, and
// returns its output and exit code.
func runMain(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), runMainEnv+"=1", "XDG_CONFIG_HOME="+t.TempDir(), "DELTA_FEATURES=",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", noRaceSleep)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}

func TestMainPrints(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		arg   string
		check func(string) bool
	}{
		{"--version", func(s string) bool { return s == "dev\n" }}, // go test embeds no version
		{"--license", func(s string) bool { return strings.HasPrefix(s, license) && strings.HasSuffix(s, thirdParty) }},
		{"--readme", func(s string) bool { return s == withoutBadges(readme(t)) }}, // stdout is no terminal
	} {
		stdout, _, code := runMain(t, dir, c.arg)
		if code != 0 || !c.check(stdout) {
			t.Errorf("%s: exit %d, stdout %.60q", c.arg, code, stdout)
		}
	}
}

// readme returns the README as embedded.
func readme(t *testing.T) string {
	t.Helper()
	b, err := docs.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every page of doc/ is a page of --readme, so that the links to it work.
func TestDocPages(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("doc", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(files, "README.md") {
		if !slices.Contains(docPages, filepath.ToSlash(f)) {
			t.Errorf("%s is not in docPages", f)
		}
	}
	if len(docPages) != len(files)+1 {
		t.Errorf("docPages %q, files %q", docPages, files)
	}
}

// What the command line passes on: jdiff's flags before log and diff, and
// all arguments after them to git, also those named like jdiff's.
func TestCommandLine(t *testing.T) {
	type started struct {
		o    options
		mode string
		args []string
	}
	for _, c := range []struct {
		args []string
		want started
	}{
		{[]string{"x0", "x1"}, started{options{}, "", []string{"x0", "x1"}}},
		{[]string{"--readme"}, started{options{readme: true}, "readme", []string{}}},
		{[]string{"-u", "diff", "--cached", "-u"}, started{options{untracked: true}, "diff", []string{"--cached", "-u"}}},
		{[]string{"--untracked", "diff"}, started{options{untracked: true}, "diff", []string{}}},
		{[]string{"-R", "log"}, started{options{autoReload: true}, "log", []string{}}},
		{[]string{"--auto-reload", "x0", "x1"}, started{options{autoReload: true}, "", []string{"x0", "x1"}}},
		// After --, files named like a command.
		{[]string{"--", "log", "log1"}, started{options{}, "", []string{"log", "log1"}}},
		{[]string{"-u", "--", "completion", "x0"}, started{options{untracked: true}, "", []string{"completion", "x0"}}},
		{[]string{"--features", "x0 x1", "log", "-20", "--readme"}, started{options{features: "x0 x1"}, "log", []string{"-20", "--readme"}}},
		{[]string{"diff", "--help"}, started{options{}, "diff", []string{"--help"}}},
	} {
		var got *started
		root := newRoot(func(o options, mode string, args []string) { got = &started{o, mode, args} })
		root.SetArgs(c.args)
		if err := root.Execute(); err != nil || got == nil || got.o != c.want.o || got.mode != c.want.mode ||
			!slices.Equal(got.args, c.want.args) && len(got.args)+len(c.want.args) > 0 {
			t.Errorf("%q: %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
}

// --help lists the flags as they are typed, with two dashes, and long names
// with one dash are no flags.
func TestMainHelp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stdout, _, code := runMain(t, dir, "--help")
	for _, want := range []string{"--version", "--readme", "-u, --untracked", "-R, --auto-reload", "--features NAMES", "\n  jdiff [flags] OLD NEW\n" +
		"  jdiff [flags] log [GIT LOG ARGS]\n  jdiff [flags] diff [GIT DIFF ARGS]\n  jdiff completion bash|zsh|fish\n"} {
		if code != 0 || !strings.Contains(stdout, want) {
			t.Errorf("--help: exit %d, lacks %q:\n%s", code, want, stdout)
		}
	}
	if strings.Contains(stdout, "[command]") {
		t.Errorf("--help names [command]:\n%s", stdout)
	}
	for _, args := range [][]string{{"-version"}, {"-readme"}, {"-license"}} {
		if _, stderr, code := runMain(t, dir, args...); code != 2 || !strings.Contains(stderr, "unknown shorthand flag") {
			t.Errorf("%v: exit %d, stderr %.60q", args, code, stderr)
		}
	}
	if _, stderr, _ := runMain(t, dir, "--readme", "x0"); !strings.HasPrefix(stderr, `jdiff: --readme takes no arguments, not "x0"`) {
		t.Errorf("--readme x0: %.60q", stderr)
	}
}

// The completion scripts: cobra's, and after log and diff git's.
func TestCompletionScripts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for shell, want := range map[string][]string{
		"bash": {"__start_jdiff()", "__git_func_wrap \"_git_${COMP_WORDS[0]}\"", "complete -o default -F __jdiff_complete jdiff"},
		// zsh: jdiff bound to _jdiff, which calls cobra's renamed one.
		"zsh":  {"#compdef jdiff\ncompdef _jdiff jdiff\n", "\n_jdiff_cobra()\n", "\n_jdiff()\n", "\n                _git\n"},
		"fish": {"function __jdiff_perform_completion", "functions -c __jdiff_prepare_completions", "complete -C"},
	} {
		stdout, _, code := runMain(t, dir, "completion", shell)
		for _, w := range want {
			if code != 0 || !strings.Contains(stdout, w) {
				t.Errorf("completion %s: exit %d, lacks %q", shell, code, w)
			}
		}
	}
	if _, stderr, code := runMain(t, dir, "completion", "x0"); code != 2 || !strings.Contains(stderr, "Usage:\n  jdiff completion bash|zsh|fish") ||
		strings.Contains(stderr, "OLD NEW") {
		t.Errorf("completion x0: exit %d, stderr %q", code, stderr)
	}
}

// What cobra completes, which the scripts ask jdiff for: the commands and
// the flags, before log and diff.
func TestComplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"__complete", ""}, []string{"log\t", "diff\t", "completion\t"}},
		{[]string{"__complete", "--"}, []string{"--readme\t", "--untracked\t", "--auto-reload\t", "--features\t", "--version\t"}},
		{[]string{"__complete", "completion", ""}, []string{"bash\n", "zsh\n", "fish\n"}},
	} {
		stdout, _, code := runMain(t, dir, c.args...)
		for _, w := range c.want {
			if code != 0 || !strings.Contains(stdout, w) {
				t.Errorf("%q: exit %d, lacks %q:\n%s", c.args, code, w, stdout)
			}
		}
	}
}

// The bash script completes jdiff's flags and commands as cobra does, and
// after log and diff as git does, also after jdiff's flags. It needs bash,
// bash-completion and git's completion.
func TestBashCompletion(t *testing.T) {
	const bashCompletion = "/usr/share/bash-completion/bash_completion"
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	for _, f := range []string{bashCompletion, "/usr/share/bash-completion/completions/git"} {
		if _, err := os.Stat(f); err != nil {
			t.Skipf("no %s", f)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// jdiff, which the script calls for cobra's completions: this test binary.
	bin := t.TempDir()
	write(t, filepath.Join(bin, "jdiff"), "#!/bin/sh\n"+runMainEnv+"=1 exec '"+exe+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "jdiff"), 0o755); err != nil {
		t.Fatal(err)
	}
	newRepo(t) // branches to complete
	git(t, "branch", "x0-branch")
	// One bash for all lines, as each jdiff it starts is slow with -race: for
	// each line its completions, then an empty line.
	script := `source ` + bashCompletion + `
source <(jdiff completion bash)
for line; do
    COMP_LINE=$line COMP_POINT=${#line}
    read -ra COMP_WORDS <<<"$line"
    COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
    COMPREPLY=()
    __jdiff_complete 2>/dev/null
    printf '%s\n' "${COMPREPLY[@]}" ""
done`
	cases := map[string]string{
		"jdiff diff --ca":               "--cached ",
		"jdiff -u diff --ca":            "--cached ",
		"jdiff --features x0 diff --ca": "--cached ",
		"jdiff log --onel":              "--oneline ",
		"jdiff diff x0-b":               "x0-branch ",
		"jdiff --rea":                   "--readme",
		"jdiff comp":                    "completion",
	}
	lines := slices.Sorted(maps.Keys(cases))
	cmd := exec.Command("bash", append([]string{"--norc", "--noprofile", "-c", script, "bash"}, lines...)...)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), noRaceSleep)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n\n"), "\n\n")
	for i, line := range lines {
		if i >= len(got) || !slices.Contains(strings.Split(got[i], "\n"), cases[line]) {
			t.Errorf("%q: completions %q; want %q", line, got, cases[line])
		}
	}
}

func TestMainUsage(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{}, {"x0"}, {"x0", "x1", "x2"}, {"--no-such-flag"}, {"--readme", "x0"}} {
		stdout, stderr, code := runMain(t, dir, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "Usage:") {
			t.Errorf("%v: exit %d, stdout %q, stderr %.60q", args, code, stdout, stderr)
		}
	}
}

func TestMainFails(t *testing.T) {
	dir := newRepo(t)
	missing := filepath.Join(dir, "missing")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{missing, "a.txt"}, "jdiff: " + missing + ": "},
		{[]string{"a.txt", missing}, "jdiff: " + missing + ": "},
		{[]string{"log", "--grep=no-such-commit"}, "jdiff: no commits\n"},
		{[]string{"log", "no-such-rev"}, "jdiff: "},
		{[]string{"diff", "no-such-rev"}, "jdiff: "},
	} {
		stdout, stderr, code := runMain(t, dir, c.args...)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, c.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want it to start with %q", c.args, code, stdout, stderr, c.want)
		}
	}
}

// newRepo changes to a new repository with a.txt, x0 to x2, committed as s0,
// and returns its directory. Subjects are not made of hex digits: the log
// shows hashes too.
func newRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	t.Chdir(dir)
	git(t, "init", "-q")
	write(t, "a.txt", "x0\nx1\nx2\n")
	commit(t, "s0")
	return dir
}

func git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commit(t *testing.T, subject string) {
	t.Helper()
	git(t, "add", ".")
	git(t, "commit", "-qm", subject)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shown returns what m draws at 80x20, without styles.
func shown(m tea.Model) string {
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return ansi.Strip(m.View().Content)
}

// pressReload presses r, which loads again, on m. The command after loading is not
// run: it is the next check for changes of what is watched, which never ends.
// pressReload presses r on m and runs what follows, as the runtime does: the
// message asking to load again, and the command listing the files, which
// runs in the background. The commands after that watch the files.
func pressReload(m tea.Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	_, cmd = m.Update(cmd())
	m.Update(cmd())
}

func TestLoadFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath, newPath := filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")
	write(t, oldPath, "x0\n")
	write(t, newPath, "x1\n")
	m, err := load(config.Default(), oldPath, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*ui.FileDiff); !ok {
		t.Fatalf("two files: %T, want the diff view", m)
	}
	if view := shown(m); !strings.Contains(view, "x0") || !strings.Contains(view, "x1") {
		t.Errorf("two files:\n%s", view)
	}
}

func TestLoadDirs(t *testing.T) {
	dir := t.TempDir()
	oldDir, newDir := filepath.Join(dir, "old"), filepath.Join(dir, "new")
	for _, d := range []string{oldDir, newDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"f00", "f01"} {
		write(t, filepath.Join(oldDir, name), "x0\n")
		write(t, filepath.Join(newDir, name), "x1\n")
	}
	m, err := load(config.Default(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*ui.Diff); !ok {
		t.Fatalf("two directories: %T, want the file selector", m)
	}
	if view := shown(m); !strings.Contains(view, "f01") || strings.Contains(view, "f02") {
		t.Errorf("two directories:\n%s", view)
	}
	// r lists the directories again.
	write(t, filepath.Join(newDir, "f02"), "x2\n")
	pressReload(m)
	if view := shown(m); !strings.Contains(view, "f02") || !strings.Contains(view, "1 new") {
		t.Errorf("after r, f02 is not listed:\n%s", view)
	}
}

func TestLoadFails(t *testing.T) {
	dir := t.TempDir()
	file, missing := filepath.Join(dir, "file"), filepath.Join(dir, "missing")
	write(t, file, "x0\n")
	for _, c := range []struct{ oldPath, newPath, want string }{
		{missing, file, missing},
		{file, missing, missing},
		{missing, missing, missing},
		{file, dir, dir}, // a file and a directory
		{dir, file, dir},
	} {
		m, err := load(config.Default(), c.oldPath, c.newPath)
		if err == nil || !strings.HasPrefix(err.Error(), c.want+": ") {
			t.Errorf("load(%q, %q) = %T, %v; want an error naming %q", c.oldPath, c.newPath, m, err, c.want)
		}
	}
}

func TestLoadLog(t *testing.T) {
	newRepo(t)
	write(t, "a.txt", "x0\nchanged\nx2\n")
	commit(t, "s1")
	m, err := loadLog(config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if view := shown(m); !strings.Contains(view, "s0") || !strings.Contains(view, "s1") {
		t.Errorf("log:\n%s", view)
	}
	// r runs git log again.
	write(t, "a.txt", "x0\nchanged\nagain\n")
	commit(t, "s2")
	pressReload(m)
	if view := shown(m); !strings.Contains(view, "s2") {
		t.Errorf("after r, s2 is not listed:\n%s", view)
	}
	// The arguments are git log's.
	m, err = loadLog(config.Default(), []string{"-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view := shown(m); !strings.Contains(view, "s2") || strings.Contains(view, "s1") {
		t.Errorf("log -1:\n%s", view)
	}

	if _, err := loadLog(config.Default(), []string{"--grep=no-such-commit"}); err == nil || err.Error() != "no commits" {
		t.Errorf("no commit selected: %v", err)
	}
	if _, err := loadLog(config.Default(), []string{"no-such-rev"}); err == nil {
		t.Error("bad revision: no error")
	}
}

// The toggle of the untracked files decides what git diff lists, also after
// the view was loaded. The only file listed is opened: a.txt.
func TestLoadDiffFollowsUntracked(t *testing.T) {
	newRepo(t)
	write(t, "a.txt", "x0\nchanged\nx2\n")
	write(t, "f00", "x3\n")
	cfg := config.Default()
	cfg.Settings.Untracked = false
	m, err := loadDiff(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view := shown(m); !strings.Contains(view, "a.txt") || strings.Contains(view, "f00") {
		t.Errorf("untracked off:\n%s", view)
	}
	cfg.Settings.Untracked = true
	pressReload(m)
	if view := shown(m); !strings.Contains(view, "a.txt") || !strings.Contains(view, "1 new") {
		t.Errorf("untracked on, f00 is not added:\n%s", view)
	}
	if _, err := loadDiff(cfg, []string{"no-such-rev"}); err == nil {
		t.Error("bad revision: no error")
	}
}

func TestWithoutBadges(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"none", "# t\n\nx0\n", "# t\n\nx0\n"},
		{"under the title", "# t\n[![a](b)](c)\n\nx0\n", "# t\n\nx0\n"},
		{"several", "[![a](b)](c)\n[![d](e)](f)\nx0\n", "x0\n"},
		{"CRLF", "# t\r\n[![a](b)](c)\r\n\r\nx0\r\n", "# t\r\n\r\nx0\r\n"},
		{"last, no newline", "x0\n[![a](b)](c)", "x0\n"},
		{"inside a line stays", "x0 [![a](b)](c)\n", "x0 [![a](b)](c)\n"},
		{"plain image stays", "![a](b)\n", "![a](b)\n"},
		{"empty", "", ""},
	} {
		if got := withoutBadges(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

var update = flag.Bool("update", false, "write "+thirdPartyFile+" instead of checking it (make licenses)")

const thirdPartyFile = "LICENSE-THIRD-PARTY"

// licenseOf returns the text of the license file in dir: LICENSE, LICENCE
// or COPYING, with any extension.
func licenseOf(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if !e.IsDir() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "LICENCE") || strings.HasPrefix(name, "COPYING")) {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			return strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") + "\n", err
		}
	}
	return "", errors.New(dir + ": no license file")
}

// thirdPartyLicenses returns the licenses of what is linked into jdiff: the
// modules of the packages main imports, not those only the tests use, and
// first the Go standard library. Its license is that of golang.org/x/sys,
// the same text, as GOROOT may lack it: Debian leaves it out.
func thirdPartyLicenses(t *testing.T) string {
	t.Helper()
	// The releases are built for these systems, which link modules of their
	// own: cobra uses mousetrap only on Windows.
	var out []byte
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd := exec.Command("go", "list", "-deps", "-f",
			"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		o, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", goos, err)
		}
		out = append(out, o...)
	}
	// A module per line, path, version and directory, which may have spaces;
	// the packages of the standard library give empty lines.
	var mods [][]string
	for _, line := range slices.Compact(slices.Sorted(slices.Values(strings.Split(string(out), "\n")))) {
		if line != "" {
			mods = append(mods, strings.Split(line, "\t"))
		}
	}
	var b strings.Builder
	block := func(heading, dir string) {
		text, err := licenseOf(dir)
		if err != nil {
			t.Fatal(err)
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("==== " + heading + " ====\n\n" + text)
	}
	sys := slices.IndexFunc(mods, func(m []string) bool { return m[0] == "golang.org/x/sys" })
	if sys < 0 {
		t.Fatal("golang.org/x/sys is not linked, whose license stands for Go's")
	}
	block("Go standard library and runtime", mods[sys][2])
	for _, m := range mods {
		block(m[0]+" "+m[1], m[2])
	}
	return b.String()
}

// LICENSE-THIRD-PARTY, which --license prints and the packages ship, holds
// the licenses of all linked modules, as they require for a binary. After a
// change of dependencies, make licenses writes it again.
func TestThirdPartyLicenses(t *testing.T) {
	want := thirdPartyLicenses(t)
	if *update {
		if err := os.WriteFile(thirdPartyFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, s := range []string{"==== Go standard library and runtime ====\n\nCopyright", "==== github.com/BurntSushi/toml v"} {
		if !strings.Contains(want, s) {
			t.Errorf("no %q", s)
		}
	}
	if strings.Contains(want, "x/exp") {
		t.Error("the license of golang.org/x/exp, which only the tests use")
	}
	if !strings.Contains(want, "==== github.com/inconshreveable/mousetrap v") {
		t.Error("no license of mousetrap, which only the Windows build links")
	}
	// git may check it out with CRLF on Windows.
	if strings.ReplaceAll(thirdParty, "\r\n", "\n") != want {
		t.Errorf("%s is not up to date with the dependencies: run make licenses", thirdPartyFile)
	}
}

// What --readme prints without a terminal: the README has its badge, the output
// neither that nor a gap in its place.
func TestReadmeBadge(t *testing.T) {
	if !strings.Contains(readme(t), "\n[![coverage](") {
		t.Error("the README has no coverage badge")
	}
	// git may check out the README with CRLF on Windows.
	got := strings.ReplaceAll(withoutBadges(readme(t)), "\r\n", "\n")
	if strings.Contains(got, "[![") {
		t.Error("--readme prints a badge")
	}
	if !strings.HasPrefix(got, "# jdiff\n\nAn interactive") {
		t.Errorf("--readme starts with %.40q", got)
	}
}
