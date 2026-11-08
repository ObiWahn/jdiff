package keys

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A keybindings.toml with its rows in another order; the same keys on other
// pages do not count.
const orderedKeys = `file_list = [{ keys = ["w"] }]
everywhere = [
    { keys = ["\\"] },
    { keys = ["b"] },
    { keys = ["f"] },
    { keys = ["s"] },
    { keys = ["o", "F8"] },
]
diff_view = [{ keys = ["t"] }]
`

func TestKeyOrder(t *testing.T) {
	p, err := Read(orderedKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`\`, "b", "f", "s", "o"}
	if got := p.Order(); !slices.Equal(got, want) {
		t.Errorf("order: %q, want %q", got, want)
	}
}

// A label past the end or empty is none, and so is an action without a row: the
// status bar leaves the key out.
func TestKeyLabel(t *testing.T) {
	r := Row{Label: []string{"on", ""}}
	for state, want := range []string{"on", "", ""} {
		if got := r.LabelIn(state); got != want {
			t.Errorf("state %d: label %q, want %q", state, got, want)
		}
	}
	if got := Builtin().Row("x0").LabelIn(0); got != "" {
		t.Errorf("no row: label %q", got)
	}
}

// The keys of keybindings.toml as Bubble Tea names them, and the page that
// looks them up first; two-way actions say which key of the row it is.
func TestKeyLookup(t *testing.T) {
	for _, c := range []struct {
		pg   Page
		k    string
		want Hit
	}{
		{DiffView, "j", Hit{"move", 0}},
		{DiffView, "up", Hit{"move", 1}},
		{DiffView, "pgdown", Hit{"page", 1}},
		{DiffView, "ctrl+b", Hit{"page", 0}},
		{DiffView, "f5", Hit{"reload", 1}},
		{DiffView, "m", Hit{"set_mark", 0}},
		{DiffView, "n", Hit{"next_file", 0}},
		{CommitList, "n", Hit{"history", 0}},
		{FileList, "p", Hit{"move", 1}},
		{FlowView, "c", Hit{"full_messages", 0}},
		{FlowView, "]", Hit{"change", 0}},
		{DiffView, "c", Hit{}},
		{Everywhere, "f8", Hit{"settings", 1}},
		{Everywhere, "F8", Hit{}},
	} {
		if got := Builtin().Key(c.pg, c.k); got != c.want {
			t.Errorf("page %d, %s: %+v, want %+v", c.pg, c.k, got, c.want)
		}
	}
}

// Placeholders stand for the keys of a row of their action, "" for none.
func TestKeyText(t *testing.T) {
	for in, want := range map[string]string{
		"{settings} close":   "o F8 close",
		"also {page:1}":      "also ctrl+b ctrl+f",
		"{page}":             "pgup pgdn",
		"{page:5}|{nothing}": "|",
		"no {braces":         "no {braces",
	} {
		if got := Builtin().Text(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The user's file replaces the rows of an action on a page, keeping their
// words, and leaves the built-in keys as they are.
func TestUserKeys(t *testing.T) {
	p, problems := Builtin().User(`commit_list = [{ action = "move", keys = ["x", "y"] }]
everywhere = [{ action = "settings", keys = ["O"] }]
`)
	if len(problems) != 0 {
		t.Fatalf("problems: %q", problems)
	}
	for _, c := range []struct {
		pg   Page
		k    string
		want Hit
	}{
		{CommitList, "x", Hit{"move", 0}},
		{CommitList, "y", Hit{"move", 1}},
		{CommitList, "j", Hit{}},
		{CommitList, "down", Hit{}}, // its second row is replaced too
		{DiffView, "j", Hit{"move", 0}},
		{DiffView, "O", Hit{"settings", 0}},
		{DiffView, "o", Hit{}},
	} {
		if got := p.Key(c.pg, c.k); got != c.want {
			t.Errorf("page %d, %s: %+v, want %+v", c.pg, c.k, got, c.want)
		}
	}
	if r := p.Row("settings"); !slices.Equal(r.Label, []string{"settings"}) || r.Help == "" {
		t.Errorf("settings lost its words: %+v", r)
	}
	if got := p.Text("{settings} close"); got != "O close" {
		t.Errorf("placeholder: %q", got)
	}
	// The settings keep their order: the row stays where it was.
	want := slices.Clone(Builtin().Order())
	want[slices.Index(want, "o")] = "O"
	if !slices.Equal(p.Order(), want) {
		t.Errorf("order: %q, want %q", p.Order(), want)
	}
	if Builtin().Key(CommitList, "j").Action != "move" || Builtin().Row("settings").Keys[0] != "o" {
		t.Errorf("the built-in keys changed")
	}
}

// A file with a problem changes no key.
func TestUserKeysProblems(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"everywhere = [", "toml:"},
		{"x = 1", "unknown x"},
		{`help = [{ note = "x" }]`, "unknown help"},
		{`everywhere = [{ action = "quit", keys = ["Q"], label = ["x"] }]`, "unknown everywhere.label"},
		{`commit_list = [{ action = "anchor", keys = ["x"] }]`, "no action anchor in commit_list"},
		{`commit_list = [{ action = "move", keys = ["x"] }]`, "move in commit_list takes two keys"},
		{`diff_view = [{ action = "pane", keys = ["j"] }]`, "key j has two actions in diff_view"},
		{`diff_view = [{ action = "pane", keys = [] }]`, "pane in diff_view has no keys"},
	} {
		p, problems := Builtin().User(c.text)
		if len(problems) == 0 || !strings.Contains(strings.Join(problems, "; "), c.want) {
			t.Errorf("%s: problems %q, want %q", c.text, problems, c.want)
		}
		if p.Key(DiffView, "j").Action != "move" || p.Key(DiffView, "tab").Action != "pane" {
			t.Errorf("%s: the keys changed", c.text)
		}
	}
}

// Without a user's file the keys are the built-in ones; with one, its keys,
// and with a problem, the built-in ones and the problem.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if p, problems := Load(dir); p != Builtin() || problems != nil {
		t.Errorf("no file: %p %q", p, problems)
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`everywhere = [{ action = "settings", keys = ["O"] }]`)
	if p, problems := Load(dir); len(problems) != 0 || p.Row("settings").Keys[0] != "O" {
		t.Errorf("a file: %q, problems %q", p.Row("settings").Keys, problems)
	}
	write(`everywhere = [{ action = "x0", keys = ["x"] }]`)
	if p, problems := Load(dir); p != Builtin() || len(problems) != 1 || problems[0] != "no action x0 in everywhere" {
		t.Errorf("a problem: %p %q", p, problems)
	}
	if Builtin().Row("settings").Keys[0] != "o" {
		t.Errorf("the built-in keys changed")
	}
}
