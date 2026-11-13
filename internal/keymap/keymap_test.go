package keymap

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// file has numbered actions x0 … and tables t0 …: x1 and x2 stand in two
// tables, x2 has two rows, a of x4 in t2 hides a of x0 on page p1.
const file = `
t0 = [
    { action = "x0", keys = ["a"], label = ["on0", "off0"], help = "h0 {t0.x1}", setting = "s0" },
    { action = "x1", keys = ["b", "c"], help = "h1", display = "b/c!" },
]
t1 = [
    { action = "x2", keys = ["d", "e"], help = "h2" },
    { action = "x2", keys = ["f", "g"], help = "h2 {x2:1}" },
    { action = "x1", keys = ["h"], help = "h1 in t1" },
    { action = "x3", keys = ["f5"], help = "h3" },
]
t2 = [
    { action = "x4", keys = ["a"], help = "h4" },
]
help = [
    { heading = "head0" },
    { action = "x0" },
    { note = "note0 {x2}" },
    { action = "x2" },
    { table = "t1", action = "x1" },
    { heading = "sub0", sub = true },
]
`

var spec = Spec{
	Tables: []string{"t0", "t1", "t2"},
	Pages:  map[string][]string{"p0": {"t1", "t0"}, "p1": {"t2", "t1", "t0"}},
	TwoWay: []string{"x2"},
}

func parse(t *testing.T, text string) *Keymap {
	t.Helper()
	m, err := Parse(text, spec)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParse(t *testing.T) {
	if p := parse(t, file).Problems(); len(p) != 0 {
		t.Errorf("problems: %q", p)
	}
	for _, c := range []struct{ text, want string }{
		{`t9 = [{ action = "x0" }]`, "unknown table t9"},
		{`t0 = [{ action = "x0", key = ["a"] }]`, "unknown t0.key"},
		{`help = [{ title = "x" }]`, "unknown help.title"},
		{`t0 = [`, "expected"},
	} {
		if _, err := Parse(c.text, spec); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) = %v, want %q", c.text, err, c.want)
		}
	}
	bad := spec
	bad.Pages = map[string][]string{"p0": {"t9"}}
	if _, err := Parse(file, bad); err == nil || err.Error() != "page p0: no table t9" {
		t.Errorf("page with unknown table: %v", err)
	}
	// Bubble Tea's names are compared as written; the old spellings pgdn
	// and F1 … become them as the file is read, also after modifiers. F
	// alone is shift+f, a key of its own.
	m, err := Parse(`t0 = [{ action = "x0", keys = ["F5", "ctrl+pgdn", "shift+F12", "F", "pgup", "+"] }]`, Spec{Tables: []string{"t0"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.Rows("t0")[0].Keys, []string{"f5", "ctrl+pgdown", "shift+f12", "F", "pgup", "+"}; !slices.Equal(got, want) {
		t.Errorf("keys %q, want %q", got, want)
	}
	if m.Key("t0", "f5").Action != "x0" || m.Key("t0", "F5").Action != "" {
		t.Errorf("f5 %+v, F5 %+v: want f5 only", m.Key("t0", "f5"), m.Key("t0", "F5"))
	}
}

func TestRow(t *testing.T) {
	m := parse(t, file)
	if r := m.Row("x1"); r.Help != "h1" {
		t.Errorf("x1 comes from t0, the first table: %+v", r)
	}
	if r := m.Row("x9"); r.Action != "" || r.Keys != nil {
		t.Errorf("x9: %+v", r)
	}
	if rows := m.RowsOf("x2"); len(rows) != 2 || rows[1].Keys[0] != "f" {
		t.Errorf("x2: %+v", rows)
	}
	if got := m.Rows("t2"); len(got) != 1 || got[0].Action != "x4" {
		t.Errorf("t2: %+v", got)
	}
	r := m.Row("x0")
	for state, want := range []string{"on0", "off0", ""} {
		if got := r.LabelOf(state); got != want {
			t.Errorf("label %d = %q, want %q", state, got, want)
		}
	}
	if got := m.Row("x1").LabelOf(0); got != "" {
		t.Errorf("label of x1 = %q", got)
	}
	if got := m.FirstKeys("t1"); !slices.Equal(got, []string{"d", "f", "h", "f5"}) {
		t.Errorf("first keys of t1 = %q", got)
	}
}

// Tables and TwoWay give the Spec's tables, in order, and its two-way
// actions; Tables returns a copy.
func TestTables(t *testing.T) {
	m := parse(t, file)
	got := m.Tables()
	if !slices.Equal(got, []string{"t0", "t1", "t2"}) {
		t.Errorf("tables = %q", got)
	}
	got[0] = "changed"
	if m.Tables()[0] != "t0" {
		t.Error("Tables shares its slice with the Spec")
	}
	if !m.TwoWay("x2") || m.TwoWay("x1") || m.TwoWay("x9") {
		t.Errorf("two-way: x2 %v, x1 %v, x9 %v; want only x2", m.TwoWay("x2"), m.TwoWay("x1"), m.TwoWay("x9"))
	}
}

func TestRowsIn(t *testing.T) {
	m := parse(t, file)
	if rows := m.RowsIn("t1", "x1"); len(rows) != 1 || rows[0].Help != "h1 in t1" {
		t.Errorf("x1 in t1: %+v", rows)
	}
	if rows := m.RowsIn("t1", "x2"); len(rows) != 2 || rows[1].Keys[0] != "f" {
		t.Errorf("x2 in t1: %+v", rows)
	}
	if rows := m.RowsIn("t2", "x1"); rows != nil {
		t.Errorf("x1 in t2: %+v", rows)
	}
	if rows := m.RowsIn("t9", "x1"); rows != nil {
		t.Errorf("x1 in t9: %+v", rows)
	}
}

func TestKey(t *testing.T) {
	m := parse(t, file)
	for _, c := range []struct {
		page, k string
		want    Hit
	}{
		{"p0", "d", Hit{"x2", 0, false}},
		{"p0", "e", Hit{"x2", 1, false}},
		{"p0", "g", Hit{"x2", 1, false}},
		{"p0", "b", Hit{"x1", 0, false}},
		{"p0", "c", Hit{"x1", 1, false}},
		{"p0", "h", Hit{"x1", 0, false}},
		{"p0", "a", Hit{"x0", 0, false}},
		{"p0", "f5", Hit{"x3", 0, false}},
		{"p0", "F5", Hit{}},
		{"p0", "z", Hit{}},
		{"p1", "a", Hit{"x4", 0, false}},
		{"p1", "b", Hit{"x1", 0, false}},
		{"t0", "a", Hit{"x0", 0, false}}, // the table of its name
		{"t0", "d", Hit{}},
		{"p9", "a", Hit{}},
	} {
		if got := m.Key(c.page, c.k); got != c.want {
			t.Errorf("Key(%s, %s) = %v, want %v", c.page, c.k, got, c.want)
		}
	}
}

func TestText(t *testing.T) {
	m := parse(t, file)
	for in, want := range map[string]string{
		"{x0}":        "a",
		"{x1} close":  "b c close",
		"{x2:1}":      "f g",
		"{x2:2}":      "",
		"{x9}":        "",
		"{x0} {x2:0}": "a d e",
		"no {Keys}":   "no {Keys}",
		"plain words": "plain words",
		"{t1.x1}":     "h",
		"{t0.x1}":     "b c",
		"{t1.x2:1}":   "f g",
		"{t2.x1}":     "",
		"{t9.x0}":     "",
	} {
		if got := m.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// Replace shows each placeholder's row as show makes it.
func TestReplace(t *testing.T) {
	m := parse(t, file)
	show := func(r Row) string { return "<" + strings.Join(r.Keys, "/") + ">" }
	for in, want := range map[string]string{
		"{x1} and {t1.x1}": "<b/c> and <h>",
		"{x2:1} {x9}":      "<f/g> ",
	} {
		if got := m.Replace(in, show); got != want {
			t.Errorf("Replace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLines(t *testing.T) {
	got := parse(t, file).Lines()
	want := []Line{
		{HelpLine{Heading: "head0"}, nil, "head0"},
		{HelpLine{Action: "x0"}, []string{"a"}, "h0 b c"},
		{HelpLine{Note: "note0 {x2}"}, nil, "note0 d e"},
		{HelpLine{Action: "x2"}, []string{"d", "e"}, "h2"},
		{HelpLine{Table: "t1", Action: "x1"}, []string{"h"}, "h1 in t1"},
		{HelpLine{Heading: "sub0", Sub: true}, nil, "sub0"},
	}
	if len(got) != len(want) {
		t.Fatalf("lines: %+v", got)
	}
	for i := range want {
		if got[i].HelpLine != want[i].HelpLine || !slices.Equal(got[i].Keys, want[i].Keys) || got[i].Text != want[i].Text {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWithUser(t *testing.T) {
	m := parse(t, file)
	u, problems := m.WithUser(`
t0 = [{ action = "x1", keys = ["i"] }]
t1 = [
    { action = "x2", keys = ["j", "k"] },
    { action = "x2", keys = ["l", "m"] },
    { action = "x2", keys = ["n", "o"] },
]`)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if r := u.Row("x1"); !slices.Equal(r.Keys, []string{"i"}) || r.Help != "h1" || r.Display != "" {
		t.Errorf("x1 keeps its words, not its display, which showed the old keys: %+v", r)
	}
	if m.Row("x1").Display != "b/c!" {
		t.Errorf("m lost its display: %+v", m.Row("x1"))
	}
	rows := u.RowsOf("x2")
	if len(rows) != 3 || rows[0].Help != "h2" || rows[1].Help != "h2 {x2:1}" || rows[2].Help != "" || rows[2].Keys[0] != "n" {
		t.Errorf("x2: %+v", rows)
	}
	if got := u.FirstKeys("t1"); !slices.Equal(got, []string{"j", "l", "n", "h", "f5"}) {
		t.Errorf("the rows stand where the first was: %q", got)
	}
	if got := u.Key("p0", "o"); got != (Hit{"x2", 1, false}) {
		t.Errorf("o: %v", got)
	}
	if got := u.Key("p0", "d"); got != (Hit{}) {
		t.Errorf("d is gone: %v", got)
	}
	if u.Text("{x1}") != "i" || len(u.Lines()) != 6 {
		t.Errorf("words follow the keys: %q", u.Text("{x1}"))
	}
	if r := m.Row("x1"); !slices.Equal(r.Keys, []string{"b", "c"}) || m.Key("p0", "d").Action != "x2" {
		t.Errorf("m changed: %+v", r)
	}
}

func TestWithUserProblems(t *testing.T) {
	m := parse(t, file)
	for _, c := range []struct{ text, want string }{
		{`t0 = [`, "expected"},
		{`t9 = [{ action = "x0", keys = ["a"] }]`, "unknown t9"},
		{`t0 = [{ action = "x0", keys = ["a"], help = "x" }]`, "unknown t0.help"},
		{`t0 = [{ action = "x0", keys = ["a"], display = "x" }]`, "unknown t0.display"},
		{`t0 = [{ action = "x2", keys = ["a", "b"] }]`, "no action x2 in t0"},
		{`t1 = [{ action = "x2", keys = ["a"] }]`, "x2 in t1 takes two keys, one each way"},
		{`t0 = [{ action = "x0", keys = [] }]`, "x0 in t0 has no keys"}, // would take away its keys
		{`t0 = [{ action = "x0" }]`, "x0 in t0 has no keys"},
		{`t1 = [{ action = "x2", keys = [] }]`, "x2 in t1 has no keys"},
		{`t0 = [{ action = "x1", keys = ["a"] }]`, "key a has two actions in t0: x0, x1"},
	} {
		got, problems := m.WithUser(c.text)
		if got != m || len(problems) != 1 || !strings.Contains(problems[0], c.want) {
			t.Errorf("WithUser(%q) = %q, want %q and no change", c.text, problems, c.want)
		}
	}
}

func TestLoad(t *testing.T) {
	m := parse(t, file)
	dir := t.TempDir()
	if got, problems := m.Load(filepath.Join(dir, "none.toml")); got != m || problems != nil {
		t.Errorf("missing file: %q", problems)
	}
	path := filepath.Join(dir, "keys.toml")
	if err := os.WriteFile(path, []byte(`t0 = [{ action = "x0", keys = ["z"] }]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, problems := m.Load(path); problems != nil || got.Key("p0", "z").Action != "x0" {
		t.Errorf("load: %q", problems)
	}
	if got, problems := m.Load(dir); got != m || len(problems) != 1 {
		t.Errorf("a directory: %q", problems)
	}
}

func TestProblems(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{`t0 = [{ action = "x0", keys = ["a"] }, { action = "x1", keys = ["b", "a"] }]`,
			[]string{"key a has two actions in t0: x0, x1"}},
		{`t0 = [{ action = "x0", keys = ["F5"] }, { action = "x1", keys = ["f5"] }]`,
			[]string{"key f5 has two actions in t0: x0, x1"}}, // two spellings, one key
		{`t0 = [{ action = "x0", keys = ["a"] }, { action = "x0", keys = ["a"] }]`, nil},
		{`t0 = [{ action = "x0", keys = ["a"] }]
          t1 = [{ action = "x1", keys = ["a"] }]`, nil},
		{`t0 = [{ action = "x0", keys = ["a"] }]
          help = [{ action = "x0" }, { action = "x9" }]`,
			[]string{"the help's action x0 has no help", "the help's action x9 has no help"}},
		{`t0 = [{ action = "x0", keys = ["a"], help = "{x0} {x0:1}", setting = "{x9}" }]
          help = [{ note = "{x8}" }]`,
			[]string{"placeholder {x0:1} has no row", "placeholder {x9} has no row", "placeholder {x8} has no row"}},
		{`t0 = [{ action = "x0", keys = ["a"], help = "{t0.x0} {t1.x0}" }]
          t1 = [{ action = "x1", keys = ["a"], help = "h" }]
          help = [{ table = "t1", action = "x1" }, { table = "t1", action = "x0" }, { table = "t0", action = "x1" }]`,
			[]string{"the help's action t1.x0 has no help", "the help's action t0.x1 has no help", "placeholder {t1.x0} has no row"}},
		{`t0 = [{ action = "x0", keys = ["a"], help = "{x0} {t0.x0} {x1:1}" }]
          t1 = [{ action = "x0", keys = ["a"], help = "h" }, { action = "x1", keys = ["b"] }]
          help = [{ action = "x0" }, { table = "t1", action = "x0" }]`,
			[]string{"the help's action x0 is in several tables: t0, t1",
				"placeholder {x0} is in several tables: t0, t1", "placeholder {x1:1} has no row"}},
		{`t0 = [{ action = "X0", keys = ["a"] }, { action = "X0", keys = ["b"] }, { action = "x-1", keys = ["c"] }]`,
			[]string{"action X0: a name is a-z, 0-9 and _", "action x-1: a name is a-z, 0-9 and _"}},
	} {
		if got := parse(t, c.text).Problems(); !slices.Equal(got, c.want) {
			t.Errorf("Problems(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	m, err := Parse(`T-0 = [{ action = "x0", keys = ["a"] }]`, Spec{Tables: []string{"T-0"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.Problems(), []string{"table T-0: a name is a-z, 0-9 and _"}; !slices.Equal(got, want) {
		t.Errorf("a table's name: %q, want %q", got, want)
	}
}

// SetUserRows changes only the rows of one action in one table, in the form
// of the file, and keeps the rest as written: comments, rows over several lines,
// odd spacing, brackets and # in strings.
func TestSetUserRows(t *testing.T) {
	odd := `# mine
"t1" = [   # rows
    {action="x2",
     keys=[ "x",   # down
            "y" ]},   # moved
    { action = "x5", keys = ["#", "]"] },
	{ action = 'x6', keys = ['T'] }
]
t0 = [{ action = "x1", keys = ["O"] }]
`
	tables := `# mine
[[t1]]
# the moves
action = "x2"
keys = [
  "x",
  "y",
]

# the pane
[[t1]]
action = "x6"   # switch
keys = ["T"]
`
	for _, c := range []struct {
		name, text, page, action string
		rows                     [][]string
		want                     string
	}{
		{"new file", "", "t1", "x6", [][]string{{"x"}},
			"t1 = [\n    { action = \"x6\", keys = [\"x\"] },\n]\n"},
		{"new page", "# mine", "t0", "x8", [][]string{{"Q"}, {"\\"}},
			"# mine\nt0 = [\n    { action = \"x8\", keys = [\"Q\"] },\n    { action = \"x8\", keys = [\"\\\\\"] },\n]\n"},
		{"over several lines", odd, "t1", "x2", [][]string{{"a", "b"}},
			strings.Replace(odd, "{action=\"x2\",\n     keys=[ \"x\",   # down\n            \"y\" ]}", `{ action = "x2", keys = ["a", "b"] }`, 1)},
		{"one row into two", odd, "t1", "x2", [][]string{{"a", "b"}, {"c", "d"}},
			strings.Replace(odd, "{action=\"x2\",\n     keys=[ \"x\",   # down\n            \"y\" ]}",
				"{ action = \"x2\", keys = [\"a\", \"b\"] },\n    { action = \"x2\", keys = [\"c\", \"d\"] }", 1)},
		{"# and ] in strings", odd, "t1", "x5", [][]string{{"[", "]"}},
			strings.Replace(odd, `{ action = "x5", keys = ["#", "]"] }`, `{ action = "x5", keys = ["[", "]"] }`, 1)},
		{"out, its line too", odd, "t1", "x5", nil,
			strings.Replace(odd, "    { action = \"x5\", keys = [\"#\", \"]\"] },\n", "", 1)},
		{"out, the comment stays", odd, "t1", "x2", nil,
			strings.Replace(odd, "{action=\"x2\",\n     keys=[ \"x\",   # down\n            \"y\" ]},", "", 1)},
		{"the last out", odd, "t1", "x6", nil,
			strings.Replace(odd, "\t{ action = 'x6', keys = ['T'] }\n", "", 1)},
		{"added after the last, without comma", odd, "t1", "x7", [][]string{{"z"}},
			strings.Replace(odd, "\t{ action = 'x6', keys = ['T'] }\n", "\t{ action = 'x6', keys = ['T'] },\n\t{ action = \"x7\", keys = [\"z\"] },\n", 1)},
		{"added in one line", odd, "t0", "x8", [][]string{{"Q"}},
			strings.Replace(odd, `[{ action = "x1", keys = ["O"] }]`, "[{ action = \"x1\", keys = [\"O\"] },\n    { action = \"x8\", keys = [\"Q\"] },\n]", 1)},
		{"into an empty array", "t1 = []\n", "t1", "x6", [][]string{{"x"}},
			"t1 = [\n    { action = \"x6\", keys = [\"x\"] },\n]\n"},
		{"a table", tables, "t1", "x2", [][]string{{"a", "b"}},
			strings.Replace(tables, "[[t1]]\n# the moves\naction = \"x2\"\nkeys = [\n  \"x\",\n  \"y\",\n]\n", "[[t1]]\naction = \"x2\"\nkeys = [\"a\", \"b\"]\n", 1)},
		{"a table out", tables, "t1", "x6", nil,
			strings.Replace(tables, "[[t1]]\naction = \"x6\"   # switch\nkeys = [\"T\"]\n", "", 1)},
		{"a table added", tables, "t1", "x7", [][]string{{"z"}},
			tables + "\n[[t1]]\naction = \"x7\"\nkeys = [\"z\"]\n"},
		{"a page after tables", tables, "t0", "x8", [][]string{{"Q"}},
			tables + "\n[[t0]]\naction = \"x8\"\nkeys = [\"Q\"]\n"},
		{"nothing to take out", odd, "t2", "x2", nil, odd},
	} {
		got, err := SetUserRows(c.text, c.page, c.action, c.rows)
		if err != nil || got != c.want {
			t.Errorf("%s: error %v\n got %q\nwant %q", c.name, err, got, c.want)
		}
	}
	for _, c := range []struct{ text, want string }{
		{"t1 = [", "toml:"},
		{"t1 = 1", "toml:"},
	} {
		if _, err := SetUserRows(c.text, "t1", "x6", [][]string{{"x"}}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: error %v, want %q", c.text, err, c.want)
		}
	}
}

// SaveUser writes the rows of one action to the user's file, keeps the rest
// of it as written, and the keys apply; a file that would have a problem is
// not written. ResetUser moves the file to .bak: the application's keys.
func TestSaveUser(t *testing.T) {
	m := parse(t, file)
	path := filepath.Join(t.TempDir(), "keys.toml")
	const mine = "# mine\nt1 = [{ action = \"x3\", keys = [\"F6\"] }]\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	u, err := m.SaveUser(path, "t0", "x1", [][]string{{"i"}})
	if err != nil {
		t.Fatal(err)
	}
	want := mine + "t0 = [\n    { action = \"x1\", keys = [\"i\"] },\n]\n"
	if got := saved(); got != want {
		t.Errorf("saved:\n%s\nwant:\n%s", got, want)
	}
	if u.Key("p0", "i").Action != "x1" || u.Key("p0", "f6").Action != "x3" {
		t.Errorf("the keys do not apply: i %+v, f6 (written F6) %+v", u.Key("p0", "i"), u.Key("p0", "f6"))
	}
	got, err := u.SaveUser(path, "t0", "x1", [][]string{{"a"}})
	if err == nil || !strings.Contains(err.Error(), "key a has two actions in t0: x0, x1") || got != u || saved() != want {
		t.Errorf("a conflict: %v, keymap kept %v:\n%s", err, got == u, saved())
	}
	if _, err := m.SaveUser(path, "t0", "x1", nil); err != nil || saved() != mine+"t0 = [\n]\n" {
		t.Errorf("back to the built-in keys: %v:\n%s", err, saved())
	}
	r, err := m.ResetUser(path)
	if bak, _ := os.ReadFile(path + ".bak"); err != nil || r != m || string(bak) != mine+"t0 = [\n]\n" {
		t.Errorf("reset: %v, .bak %q", err, bak)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("reset: the file is still there")
	}
	if _, err := m.ResetUser(path); err == nil {
		t.Error("a reset without a file: no error")
	}
}

// A key that ends in <letter> is the key before it, after which the
// application reads a letter: Hit says so. It is one key with the key alone
// (a conflict); the old spellings become Bubble Tea's before it too. Only
// <letter> at the end counts: < and ctrl+< are keys of their own.
func TestKeyLetter(t *testing.T) {
	s := Spec{Tables: []string{"t0"}}
	m, err := Parse(`t0 = [
    { action = "x0", keys = ["m<letter>", "ctrl+pgdn<letter>"] },
    { action = "x1", keys = ["'<letter>", "<"] },
    { action = "x2", keys = ["ctrl+<", "<<"] },
]`, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		k    string
		want Hit
	}{
		{"m", Hit{"x0", 0, true}},
		{"ctrl+pgdown", Hit{"x0", 1, true}},
		{"'", Hit{"x1", 0, true}},
		{"<", Hit{"x1", 1, false}},
		{"ctrl+<", Hit{"x2", 0, false}},
		{"m<letter>", Hit{}}, // the text is no key press
	} {
		if got := m.Key("t0", c.k); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.k, got, c.want)
		}
	}
	if got := m.Rows("t0")[0].Keys; !slices.Equal(got, []string{"m<letter>", "ctrl+pgdown<letter>"}) {
		t.Errorf("keys %q: want <letter> kept, pgdn as pgdown", got)
	}
	m, err = Parse(`t0 = [{ action = "x0", keys = ["m<letter>"] }, { action = "x1", keys = ["m"] }]`, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.Conflicts(), []string{"key m has two actions in t0: x0, x1"}; !slices.Equal(got, want) {
		t.Errorf("m<letter> and m: %q, want %q", got, want)
	}
}

// A key that cannot be a single key press would never match: Problems names
// it in the application's file, WithUser refuses it in a user's.
func TestKeyPress(t *testing.T) {
	s := Spec{Tables: []string{"t0"}}
	good := []string{"a", "F", "<", "+", "?", "日", "ä", "ctrl+<", "ctrl++", "alt+1", "shift+tab", "f12", "pgdown",
		"ctrl+pgdown", "space", "ctrl+shift+alt+up", "m<letter>", "<<letter>", "kp5", "backspace"}
	bad := []string{"/<<>", "Enter", "ctrl+", "foo+a", "ab-c", "<letter>", "a b", ""}
	for _, k := range good {
		m, err := Parse(`t0 = [{ action = "x0", keys = [`+strconv.Quote(k)+`] }]`, s)
		if err != nil {
			t.Fatal(err)
		}
		if p := m.Problems(); len(p) != 0 {
			t.Errorf("%q: problems %q, want none", k, p)
		}
	}
	for _, k := range bad {
		m, err := Parse(`t0 = [{ action = "x0", keys = [`+strconv.Quote(k)+`] }]`, s)
		if err != nil {
			t.Fatal(err)
		}
		if p, want := m.Problems(), "key "+k+" in t0 is no key press"; !slices.Contains(p, want) {
			t.Errorf("%q: problems %q, want %q", k, p, want)
		}
	}
	m := parse(t, file)
	if got, problems := m.WithUser(`t0 = [{ action = "x0", keys = ["/<<>"] }]`); got != m || !slices.Contains(problems, "key /<<> in t0 is no key press") {
		t.Errorf("a user's /<<>: %q, keymap kept %v", problems, got == m)
	}
}
