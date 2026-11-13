package keys

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jdiff/internal/keymap"
)

// The keys of keybindings.toml as Bubble Tea names them, and the page that
// looks them up first; two-way actions say which key of the row it is.
func TestKeyLookup(t *testing.T) {
	for _, c := range []struct {
		pg   string
		k    string
		want keymap.Hit
	}{
		{DiffView, "j", keymap.Hit{Action: "move", I: 0}},
		{DiffView, "up", keymap.Hit{Action: "move", I: 1}},
		{DiffView, "pgdown", keymap.Hit{Action: "page", I: 1}},
		{DiffView, "pgup", keymap.Hit{Action: "page", I: 0}},
		{DiffView, "ctrl+b", keymap.Hit{}},
		{DiffView, "f5", keymap.Hit{Action: "reload", I: 1}},
		{DiffView, "m", keymap.Hit{Action: "set_mark", I: 0, Letter: true}},
		{DiffView, "n", keymap.Hit{Action: "next_file", I: 0}},
		{CommitList, "n", keymap.Hit{Action: "history", I: 0}},
		{FileList, "p", keymap.Hit{Action: "move", I: 1}},
		{FlowView, "c", keymap.Hit{Action: "full_messages", I: 0}},
		{FlowView, "]", keymap.Hit{Action: "change", I: 0}},
		{DiffView, "c", keymap.Hit{}},
		{Everywhere, "f8", keymap.Hit{Action: "settings", I: 1}},
		{Everywhere, "F8", keymap.Hit{}},
	} {
		if got := Builtin().Key(c.pg, c.k); got != c.want {
			t.Errorf("page %s, %s: %+v, want %+v", c.pg, c.k, got, c.want)
		}
	}
}

// Placeholders stand for the keys of a row of their action, "" for none.
func TestKeyText(t *testing.T) {
	for in, want := range map[string]string{
		"{settings} close":   "o F8 close",
		"also {move:1}":      "also down up",
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
	p, problems := Builtin().WithUser(`commit_list = [{ action = "move", keys = ["x", "y"] }]
everywhere = [{ action = "settings", keys = ["O"] }]
`)
	if len(problems) != 0 {
		t.Fatalf("problems: %q", problems)
	}
	for _, c := range []struct {
		pg   string
		k    string
		want keymap.Hit
	}{
		{CommitList, "x", keymap.Hit{Action: "move", I: 0}},
		{CommitList, "y", keymap.Hit{Action: "move", I: 1}},
		{CommitList, "j", keymap.Hit{}},
		{CommitList, "down", keymap.Hit{}}, // its second row is replaced too
		{DiffView, "j", keymap.Hit{Action: "move", I: 0}},
		{DiffView, "O", keymap.Hit{Action: "settings", I: 0}},
		{DiffView, "o", keymap.Hit{}},
	} {
		if got := p.Key(c.pg, c.k); got != c.want {
			t.Errorf("page %s, %s: %+v, want %+v", c.pg, c.k, got, c.want)
		}
	}
	if r := p.Row("settings"); !slices.Equal(r.Label, []string{"settings"}) || r.Help == "" {
		t.Errorf("settings lost its words: %+v", r)
	}
	if got := (&Map{p}).Text("{settings} close"); got != "O close" {
		t.Errorf("placeholder: %q", got)
	}
	// The settings keep their order: the row stays where it was.
	want := Builtin().FirstKeys(Everywhere)
	want[slices.Index(want, "o")] = "O"
	if got := p.FirstKeys(Everywhere); !slices.Equal(got, want) {
		t.Errorf("order: %q, want %q", got, want)
	}
	if Builtin().Key(CommitList, "j").Action != "move" || Builtin().Row("settings").Keys[0] != "o" {
		t.Errorf("the built-in keys changed")
	}
}

// A file with a problem changes no key.
func TestUserKeysProblems(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"everywhere = [", "toml:"},
		{"x = 1", "incompatible types"}, // keymap: toml's error, jdiff said unknown x
		{`help = [{ note = "x" }]`, "unknown help"},
		{`everywhere = [{ action = "quit", keys = ["Q"], label = ["x"] }]`, "unknown everywhere.label"},
		{`commit_list = [{ action = "anchor", keys = ["x"] }]`, "no action anchor in commit_list"},
		{`commit_list = [{ action = "move", keys = ["x"] }]`, "move in commit_list takes two keys"},
		{`diff_view = [{ action = "pane", keys = ["j"] }]`, "key j has two actions in diff_view"},
		{`diff_view = [{ action = "pane", keys = [] }]`, "pane in diff_view has no keys"},
	} {
		p, problems := Builtin().WithUser(c.text)
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
	path := filepath.Join(t.TempDir(), FileName)
	if p, problems := Load(path); p != Builtin() || problems != nil {
		t.Errorf("no file: %p %q", p, problems)
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`everywhere = [{ action = "settings", keys = ["O"] }]`)
	if p, problems := Load(path); len(problems) != 0 || p.Row("settings").Keys[0] != "O" {
		t.Errorf("a file: %q, problems %q", p.Row("settings").Keys, problems)
	}
	write(`everywhere = [{ action = "x0", keys = ["x"] }]`)
	if p, problems := Load(path); p != Builtin() || len(problems) != 1 || problems[0] != "no action x0 in everywhere" {
		t.Errorf("a problem: %p %q", p, problems)
	}
	if Builtin().Row("settings").Keys[0] != "o" {
		t.Errorf("the built-in keys changed")
	}
}

// Spelling writes Bubble Tea's names as keybindings.toml did and the help
// shows them, also after modifiers and before <letter>.
func TestSpelling(t *testing.T) {
	for k, want := range map[string]string{
		"pgdown": "pgdn", "f5": "F5", "f12": "F12", "ctrl+pgdown": "ctrl+pgdn", "shift+f3": "shift+F3",
		"f": "f", "ctrl+f": "ctrl+f", "j": "j", "<": "<", "ctrl++": "ctrl++", "m<letter>": "m<letter>",
	} {
		if got := Spelling(k); got != want {
			t.Errorf("%s: %s, want %s", k, got, want)
		}
	}
}
