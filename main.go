// Command jdiff is an interactive side-by-side diff viewer with manual line
// alignment.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
	"jdiff/internal/theme"
	"jdiff/internal/ui"
)

//go:embed README.md
var readme string

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	showReadme := flag.Bool("readme", false, "print the README and exit")
	features := flag.String("features", "",
		"delta features to use instead of delta.features: `NAMES` of [delta \"name\"] sections of\n"+
			"your git config, separated by spaces. They only shape the \"delta\" theme, which is used\n"+
			"unless main.toml names another. DELTA_FEATURES, when set, wins; with a leading + it adds\n"+
			"to them.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %[1]s [--version] [--readme] [--features NAMES] OLD NEW\n"+
			"       %[1]s [--features NAMES] log [GIT LOG ARGS]\n"+
			"       %[1]s [--features NAMES] diff [GIT DIFF ARGS]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(resolveVersion())
		return
	}
	if *showReadme {
		fmt.Print(readme)
		return
	}
	mode := ""
	if flag.NArg() > 0 && (flag.Arg(0) == "log" || flag.Arg(0) == "diff") {
		mode = flag.Arg(0)
	}
	if mode == "" && flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}

	dir, err := config.Dir()
	if err != nil {
		dir = "" // no home: defaults only
	}
	cfg, err := config.Load(dir, theme.ReadGitConfig(), os.Getenv("DELTA_FEATURES"), *features)
	if err != nil {
		fatal(err)
	}
	repo, err := gitsrc.Repo{}.TopLevel()
	if err != nil {
		repo = "" // not in a repository: marks with absolute paths
	}
	cfg.LoadMarks(repo)

	var m tea.Model
	switch mode {
	case "log":
		m, err = loadLog(cfg, flag.Args()[1:])
	case "diff":
		m, err = loadDiff(cfg, flag.Args()[1:])
	default:
		m, err = load(cfg, flag.Arg(0), flag.Arg(1))
	}
	if err != nil {
		fatal(err)
	}
	// d replaces whatever is shown by the files of git diff, which runs in
	// the current directory; the search bar and the popups are put around
	// the new view too.
	wrap := func(m tea.Model) tea.Model {
		return ui.WithSearch(cfg, ui.WithHelp(cfg, ui.WithSettings(cfg, ui.WithMarks(cfg, m))))
	}
	page := ui.WithSizeGuard(cfg, ui.WithDiffSwitch(cfg, m, wrap, gitDiff(cfg)))
	// 16 color mode: colors are drawn as ANSI colors, so the terminal
	// palette applies. Terminals without color support stay without.
	var opts []tea.ProgramOption
	if colorprofile.Detect(os.Stdout, os.Environ()) > colorprofile.ANSI {
		opts = append(opts, tea.WithColorProfile(colorprofile.ANSI))
	}
	if _, err := tea.NewProgram(page, opts...).Run(); err != nil {
		fatal(err)
	}
}

// load opens the file selector for two directories, as passed by
// git difftool --dir-diff, and the diff view for two files; both reload
// files that change on disk.
func load(cfg *config.Config, oldPath, newPath string) (tea.Model, error) {
	oldInfo, errOld := os.Stat(oldPath)
	newInfo, errNew := os.Stat(newPath)
	if errOld == nil && errNew == nil && oldInfo.IsDir() && newInfo.IsDir() {
		files, err := source.LoadDirs(oldPath, newPath)
		if err != nil {
			return nil, err
		}
		relist := func() ([]source.File, error) { return source.LoadDirs(oldPath, newPath) }
		return ui.NewApp(cfg, files).WithRelist(relist), nil
	}

	// Path names the file in messages; the panes show the paths as given.
	f := source.File{Path: filepath.Base(newPath), OldFile: oldPath, NewFile: newPath, InOld: true, InNew: true}
	var err error
	if f.Old, f.OldStamp, err = source.Read(oldPath); err != nil {
		return nil, fmt.Errorf("%s: %w", oldPath, err)
	}
	if f.New, f.NewStamp, err = source.Read(newPath); err != nil {
		return nil, fmt.Errorf("%s: %w", newPath, err)
	}
	return ui.NewWatched(cfg, f), nil
}

// loadLog opens the commit list for the commits git log selects with args.
func loadLog(cfg *config.Config, args []string) (tea.Model, error) {
	repo := gitsrc.Repo{}
	commits, err := repo.Log(args)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("no commits")
	}
	relist := func() (string, []gitsrc.Commit, error) {
		commits, err := repo.Log(args)
		return repo.Branch(), commits, err
	}
	return ui.NewLog(cfg, repo.Branch(), commits, gitsrc.Reversed(args), repo.Files, repo.Counts).WithRelist(relist), nil
}

// loadDiff opens the file selector for the files git diff compares with
// args; files of the working tree are reloaded when they change on disk.
func loadDiff(cfg *config.Config, args []string) (tea.Model, error) {
	diff := gitDiff(cfg)
	files, err := diff(args)
	if err != nil {
		return nil, err
	}
	return ui.NewApp(cfg, files).WithGitDiff(args, diff), nil
}

// gitDiff returns the function running git diff, which follows the toggle of
// the untracked files.
func gitDiff(cfg *config.Config) ui.DiffFunc {
	repo := gitsrc.Repo{}
	return func(args []string) ([]source.File, error) {
		if cfg.Settings.Untracked {
			return repo.DiffUntracked(args)
		}
		return repo.Diff(args)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jdiff:", err)
	os.Exit(1)
}

func resolveVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return "dev"
	}
	// Main.Version is the tag, or a pseudo-version containing the revision.
	v := strings.TrimSuffix(bi.Main.Version, "+dirty")
	if v == "" || v == "(devel)" || strings.Contains(v, rev[:min(len(rev), 12)]) {
		v = rev[:min(len(rev), 7)]
	}
	return v + dirty
}
