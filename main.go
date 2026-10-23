// Command jdiff is an interactive side-by-side diff viewer with manual line
// alignment.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
	"jdiff/internal/theme"
	"jdiff/internal/ui"
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	features := flag.String("features", "", "delta features (themes) to use instead of delta.features")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %[1]s [--version] [--features NAMES] OLD NEW\n"+
			"       %[1]s [--features NAMES] log [GIT LOG ARGS]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(resolveVersion())
		return
	}
	isLog := flag.NArg() > 0 && flag.Arg(0) == "log"
	if !isLog && flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}

	// 16 color mode: colors render as ANSI colors, so the terminal palette
	// applies. Terminals without color support stay without.
	if lipgloss.ColorProfile() != termenv.Ascii {
		lipgloss.SetColorProfile(termenv.ANSI)
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
	if isLog {
		m, err = loadLog(cfg, flag.Args()[1:])
	} else {
		m, err = load(cfg, flag.Arg(0), flag.Arg(1))
	}
	if err != nil {
		fatal(err)
	}
	if _, err := tea.NewProgram(ui.WithHelp(cfg, m), tea.WithAltScreen()).Run(); err != nil {
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
		return ui.NewApp(cfg, files), nil
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
	return ui.NewLog(cfg, repo.Branch(), commits, repo.Files), nil
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
