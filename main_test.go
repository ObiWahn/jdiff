package main

import (
	"errors"
	"flag"
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
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
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
		{"--readme", func(s string) bool { return s == withoutBadges(readme) }},
	} {
		stdout, _, code := runMain(t, dir, c.arg)
		if code != 0 || !c.check(stdout) {
			t.Errorf("%s: exit %d, stdout %.60q", c.arg, code, stdout)
		}
	}
}

func TestMainUsage(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{}, {"x0"}, {"x0", "x1", "x2"}, {"--no-such-flag"}} {
		stdout, stderr, code := runMain(t, dir, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: ") {
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
func pressReload(m tea.Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
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
	if _, ok := m.(*ui.Model); !ok {
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
	if _, ok := m.(*ui.App); !ok {
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
	out, err := exec.Command("go", "list", "-deps", "-f",
		"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
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
	// git may check it out with CRLF on Windows.
	if strings.ReplaceAll(thirdParty, "\r\n", "\n") != want {
		t.Errorf("%s is not up to date with the dependencies: run make licenses", thirdPartyFile)
	}
}

// What --readme prints: the README has its badge, the output neither that nor
// a gap in its place.
func TestReadmeBadge(t *testing.T) {
	if !strings.Contains(readme, "\n[![coverage](") {
		t.Error("the README has no coverage badge")
	}
	// git may check out the README with CRLF on Windows.
	got := strings.ReplaceAll(withoutBadges(readme), "\r\n", "\n")
	if strings.Contains(got, "[![") {
		t.Error("--readme prints a badge")
	}
	if !strings.HasPrefix(got, "# jdiff\n\nAn interactive") {
		t.Errorf("--readme starts with %.40q", got)
	}
}
