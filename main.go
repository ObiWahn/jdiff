// Command jdiff is an interactive side-by-side diff viewer with manual line
// alignment.
package main

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
	"jdiff/internal/theme"
	"jdiff/internal/ui"
)

//go:embed LICENSE
var license string

// thirdParty holds the licenses of the modules linked into jdiff, which
// they require a binary to carry; make licenses writes it.
//
//go:embed LICENSE-THIRD-PARTY
var thirdParty string

// docs holds the documentation that --readme shows: the packages install only
// the binary, so it is the only copy at hand. doc/config.md links to the
// built-in keybindings.toml.
//
//go:embed README.md doc/*.md internal/ui/keybindings.toml
var docs embed.FS

// docPages are the pages of docs in the order of n and p; --readme starts
// with the first.
var docPages = []string{"README.md", "doc/usage.md", "doc/features.md", "doc/install.md", "doc/config.md",
	"doc/keybindings.md", "doc/development.md"}

func main() {
	cmd, err := newRoot(start).ExecuteC()
	if err == nil {
		return
	}
	// The errors of running a view end in fatal; those left are of the
	// command line, of the command cmd.
	fmt.Fprintf(os.Stderr, "jdiff: %v\n\n%s", err, cmd.UsageString())
	os.Exit(2)
}

// options are the flags of the command line.
type options struct {
	version, license, readme, untracked bool
	features                            string
}

// newRoot returns the command line: jdiff OLD NEW, jdiff log and jdiff diff,
// whose arguments go to git as they are. jdiff's flags come before log and
// diff. It calls start with what to show, see start.
func newRoot(start func(o options, mode string, args []string)) *cobra.Command {
	var o options
	cobra.EnableCommandSorting = false // log, diff, completion
	root := &cobra.Command{
		Use:   "jdiff [flags] OLD NEW",
		Short: "An interactive diff viewer for the terminal, with manual line alignment, that follows your edits",
		Example: "  jdiff old.txt new.txt     compare two files, or two directories\n" +
			"  jdiff log -20             browse the last 20 commits\n" +
			"  jdiff -u diff HEAD        what git diff HEAD shows, and the untracked files\n" +
			"  jdiff -- log log1         compare files named like a command",
		Args: func(cmd *cobra.Command, args []string) error {
			for _, f := range []struct {
				on   bool
				name string
			}{{o.version, "version"}, {o.license, "license"}, {o.readme, "readme"}} {
				if f.on && len(args) > 0 {
					return fmt.Errorf("--%s takes no arguments, not %q", f.name, args[0])
				}
				if f.on {
					return nil
				}
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := ""
			switch {
			case o.version:
				fmt.Println(resolveVersion())
				return nil
			case o.license:
				printLicense()
				return nil
			case o.readme:
				mode = "readme"
			}
			start(o, mode, args)
			return nil
		},
		SilenceErrors:    true,
		SilenceUsage:     true,
		TraverseChildren: true, // the flags before log and diff
	}
	f := root.Flags()
	f.BoolVar(&o.version, "version", false, "print the version")
	f.BoolVar(&o.license, "license", false, "print the license and those of the libraries")
	f.BoolVar(&o.readme, "readme", false, "show the README and the documentation it links to; print the README as markdown\n"+
		"when the output is no terminal")
	f.BoolVarP(&o.untracked, "untracked", "u", false, "also show the untracked files in a git diff; u in the view switches them.\n"+
		"Neither is saved")
	f.StringVar(&o.features, "features", "", "delta features to use instead of delta.features: `NAMES` of [delta \"name\"]\n"+
		"sections of your git config, separated by spaces. They only shape the \"delta\" theme,\n"+
		"which is used unless main.toml names another. DELTA_FEATURES, when set, wins; with a\n"+
		"leading + it adds to them.")
	for _, c := range []struct{ name, use, short string }{
		{"log", "log [GIT LOG ARGS]", "browse the commits that git log lists"},
		{"diff", "diff [GIT DIFF ARGS]", "show the files that git diff shows"},
	} {
		root.AddCommand(&cobra.Command{
			Use:                c.use,
			Short:              c.short,
			DisableFlagParsing: true, // all of them are git's
			RunE: func(cmd *cobra.Command, args []string) error {
				start(o, c.name, args)
				return nil
			},
		})
	}
	root.AddCommand(&cobra.Command{
		Use:   "completion bash|zsh|fish",
		Short: "print the script that completes jdiff's arguments in the shell",
		Long: "Prints the script that completes jdiff's flags and arguments in the shell; after log and diff\n" +
			"it completes as git does, when git's completion is installed. To use it, e.g.:\n\n" +
			"  bash: source <(jdiff completion bash)       in ~/.bashrc\n" +
			"  zsh:  source <(jdiff completion zsh)        in ~/.zshrc, after compinit\n" +
			"  fish: jdiff completion fish | source        in ~/.config/fish/config.fish\n\n" +
			"The packages install it, so that this is not needed.",
		ValidArgs: []string{"bash", "zsh", "fish"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printCompletion(cmd.OutOrStdout(), cmd.Root(), args[0])
		},
	})
	// A usage line for each command, jdiff's flags before log and diff, and
	// no "jdiff [command] --help": the help of log and diff is git's.
	usage := strings.Replace(root.UsageTemplate(), "\n  {{.CommandPath}} [command]{{end}}",
		"{{range .Commands}}{{if .IsAvailableCommand}}\n  {{$.CommandPath}} {{if .DisableFlagParsing}}[flags] {{end}}{{.Use}}"+
			"{{end}}{{end}}{{end}}", 1)
	usage = strings.Replace(usage, "{{if .HasAvailableSubCommands}}\n\nUse \"{{.CommandPath}} [command] --help\" "+
		"for more information about a command.{{end}}", "", 1)
	root.SetUsageTemplate(usage)
	root.SetHelpCommand(&cobra.Command{Hidden: true}) // --help does it
	root.CompletionOptions.DisableDefaultCmd = true   // the one above
	return root
}

// shellParts holds what printCompletion adds to cobra's scripts, git.bash,
// git.zsh and git.fish: after log and diff, git's completion takes over.
//
//go:embed shell
var shellParts embed.FS

// printCompletion writes the completion script of root for shell to w:
// cobra's, then its part of shellParts. zsh calls a completion function by
// the name of its file, _jdiff, so cobra's _jdiff is renamed for the one of
// shellParts to call; jdiff stays bound to _jdiff.
func printCompletion(w io.Writer, root *cobra.Command, shell string) error {
	var b bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, true)
	case "zsh":
		err = root.GenZshCompletion(&b)
	case "fish":
		err = root.GenFishCompletion(&b, true)
	}
	if err != nil {
		return err
	}
	script := b.String()
	if shell == "zsh" {
		script = strings.ReplaceAll(script, "_jdiff", "_jdiff_cobra")
		script = strings.Replace(script, "compdef _jdiff_cobra jdiff", "compdef _jdiff jdiff", 1)
	}
	part, err := shellParts.ReadFile("shell/git." + shell)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, script+"\n"+string(part))
	return err
}

// start shows what mode asks for: the README, git log or git diff with the
// git arguments args, or else the two files or directories of args.
func start(o options, mode string, args []string) {
	if mode == "readme" && !term.IsTerminal(os.Stdout.Fd()) {
		src, err := docs.ReadFile(docPages[0])
		if err != nil {
			fatal(err)
		}
		fmt.Print(withoutBadges(string(src)))
		return
	}

	dir, err := config.Dir()
	if err != nil {
		dir = "" // no home: defaults only
	}
	cfg, err := config.Load(dir, theme.ReadGitConfig(), os.Getenv("DELTA_FEATURES"), o.features)
	if err != nil {
		fatal(err)
	}
	repo, err := gitsrc.Repo{}.TopLevel()
	if err != nil {
		repo = "" // not in a repository: marks with absolute paths
	}
	if o.untracked {
		cfg.Settings.Untracked = true
	}
	cfg.LoadMarks(repo)
	ui.LoadKeys(cfg)

	if mode == "readme" {
		run(ui.WithSizeGuard(cfg, ui.WithSearch(cfg, ui.WithHelp(cfg, ui.NewDoc(cfg, docs, docPages, 0)))))
		return
	}
	var m tea.Model
	switch mode {
	case "log":
		m, err = loadLog(cfg, args)
	case "diff":
		m, err = loadDiff(cfg, args)
	default:
		m, err = load(cfg, args[0], args[1])
	}
	if err != nil {
		fatal(err)
	}
	ui.OrderSettings()
	// d replaces whatever is shown by the files of git diff, which runs in
	// the current directory; the search bar and the popups are put around
	// the new view too.
	wrap := func(m tea.Model) tea.Model {
		return ui.WithSearch(cfg, ui.WithHelp(cfg, ui.WithSettings(cfg, ui.WithMarks(cfg, m))))
	}
	run(ui.WithSizeGuard(cfg, ui.WithDiffSwitch(cfg, m, wrap, gitDiff).WithAutoReload(gitsrc.Repo{}.Status, gitsrc.Repo{}.WatchPaths)))
}

// run runs the program that shows page. 16 color mode: colors are drawn as
// ANSI colors, so the terminal palette applies. Terminals without color
// support stay without.
func run(page tea.Model) {
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
	files, err := gitDiff(args, cfg.Settings.Untracked)
	if err != nil {
		return nil, err
	}
	return ui.NewApp(cfg, files).WithGitDiff(args, gitDiff), nil
}

// gitDiff runs git diff with args, with the untracked files when untracked
// is set; see ui.DiffFunc.
func gitDiff(args []string, untracked bool) ([]source.File, error) {
	if untracked {
		return gitsrc.Repo{}.DiffUntracked(args)
	}
	return gitsrc.Repo{}.Diff(args)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jdiff:", err)
	os.Exit(1)
}

// withoutBadges drops the lines of badges from a page of the documentation:
// images to load are of no use in a terminal.
func withoutBadges(readme string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(readme, "\n") {
		if !strings.HasPrefix(line, "[![") {
			b.WriteString(line)
		}
	}
	return b.String()
}

// printLicense prints jdiff's license and those of the modules linked into it.
func printLicense() {
	fmt.Print(license, "\njdiff is built with these libraries, under their licenses:\n\n", thirdParty)
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
