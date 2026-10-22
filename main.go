// Command jdiff is an interactive side-by-side diff viewer with manual line
// alignment.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/internal/theme"
	"jdiff/internal/ui"
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	features := flag.String("features", "", "delta features (themes) to use instead of delta.features")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [--version] [--features NAMES] OLD NEW\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(resolveVersion())
		return
	}
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}

	// 16 color mode: colors render as ANSI colors, so the terminal palette
	// applies. Terminals without color support stay without.
	if lipgloss.ColorProfile() != termenv.Ascii {
		lipgloss.SetColorProfile(termenv.ANSI)
	}
	cfg := theme.ReadGitConfig()
	th := theme.Load(cfg, theme.Features(cfg, os.Getenv("DELTA_FEATURES"), *features))

	m, err := load(th, flag.Arg(0), flag.Arg(1))
	if err != nil {
		fatal(err)
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fatal(err)
	}
}

// load opens the file selector for two directories, as passed by
// git difftool --dir-diff, and the diff view for two files.
func load(th *theme.Theme, oldPath, newPath string) (tea.Model, error) {
	oldInfo, errOld := os.Stat(oldPath)
	newInfo, errNew := os.Stat(newPath)
	if errOld == nil && errNew == nil && oldInfo.IsDir() && newInfo.IsDir() {
		files, err := source.LoadDirs(oldPath, newPath)
		if err != nil {
			return nil, err
		}
		return ui.NewApp(th, settings.Default(), files), nil
	}

	oldLines, err := source.ReadLines(oldPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", oldPath, err)
	}
	newLines, err := source.ReadLines(newPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", newPath, err)
	}
	return ui.New(th, settings.Default(), oldPath, newPath, oldLines, newLines), nil
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
