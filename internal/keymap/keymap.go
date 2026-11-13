// Package keymap binds the keys of an application's pages to actions, read
// from a TOML file, so that the code switches on actions, never on keys.
//
// The file has tables of rows, each named in Spec.Tables, and the table help,
// the lines of a help page:
//
//	everywhere = [
//	    { action = "quit", keys = ["q", "ctrl+c"], label = ["quit"], help = "quit" },
//	]
//	list = [
//	    { action = "move", keys = ["j", "k"], help = "move" },
//	    { action = "move", keys = ["down", "up"], help = "move" },
//	]
//	help = [
//	    { heading = "Everywhere" },
//	    { action = "quit" },
//	    { note = "{quit} also closes the popups" },
//	    { heading = "Popups", sub = true },
//	]
//
// A row: action names what the keys do, and several rows may share one; keys
// are the keys, help describes them, label is the word of a status bar by the
// state of the action, setting is what a setting does while on, display shows
// the keys in a help instead of their names (none: the application shows the
// names). A user's file changes only keys, never these words; a row whose
// keys it replaces loses its display, which showed the keys replaced. In help,
// setting and note, {action} stands for the keys of the first row of action,
// {action:1} for those of its second row, so that the words follow the keys a
// user's file sets (WithUser), which changes only keys. {table.action} and
// {table.action:1} take the rows of action in table, for an action that
// stands in several tables; so does a help line with a table:
//
//	{ table = "list", action = "move" }
//
// Tables and actions are named with a-z, 0-9 and _ only, the names a
// placeholder takes (Problems).
//
// Nothing here draws. A file names each key as the UI framework the
// application uses names a key press (Bubble Tea: tea.KeyPressMsg.String(),
// e.g. pgdown, f1, ctrl+pgdown), since Key compares the two as written; the
// only names translated are the old spellings pgdn and F1 … (also after
// modifiers, ctrl+pgdn), which become Bubble Tea's pgdown and f1 … as a file
// is read, so that each key has one name to compare, find twice or write
// (SaveUser). A key that ends in <letter> (m<letter>, '<letter>) is the key
// before it, after which the application reads a letter typed next: Key says
// so in Hit.Letter; only <letter> at the end counts, < and ctrl+< are keys of
// their own. A key that cannot be one key press (a word that is no key's
// name, an unknown modifier) would never match: Problems names it, WithUser
// refuses it. A help showing a key otherwise takes it from the row's display
// or from the framework's helper (teakeys).
package keymap

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Row is a row of a table.
type Row struct {
	Action  string
	Keys    []string
	Label   []string
	Help    string
	Setting string
	Display string
}

// LabelOf returns the word of the status bar in state, "" when there is none.
func (r Row) LabelOf(state int) string {
	if state < len(r.Label) {
		return r.Label[state]
	}
	return ""
}

// HelpLine is a line of the table help: a heading stands alone, one with Sub
// under the heading above it, a note goes on from the line above, an action
// gives its keys and its help: the action of Table, or of the first table
// that has it when Table is empty.
type HelpLine struct {
	Heading, Note, Table, Action string
	Sub                          bool
}

// Spec describes the tables of a file and how keys are looked up in them.
type Spec struct {
	// Tables names the tables, in the order an action is looked up (Row):
	// the first table with the action gives its keys and words.
	Tables []string
	// Pages lists for each page the tables its keys are looked up in, in
	// order; a page not in Pages looks in the table of its own name.
	Pages map[string][]string
	// TwoWay lists the actions whose rows take two keys, one for each way.
	TwoWay []string
}

// Keymap is a file read with its Spec. Its methods do not change it.
type Keymap struct {
	spec   Spec
	tables map[string][]Row
	help   []HelpLine
}

// helpTable is the table of the help's lines.
const helpTable = "help"

// Parse reads text, a file of the tables of s. A table, a field or a table of
// a page that s does not name is an error.
func Parse(text string, s Spec) (*Keymap, error) {
	for page, tables := range s.Pages {
		for _, t := range tables {
			if !slices.Contains(s.Tables, t) {
				return nil, fmt.Errorf("page %s: no table %s", page, t)
			}
		}
	}
	var raw map[string]toml.Primitive
	md, err := toml.Decode(text, &raw)
	if err != nil {
		return nil, err
	}
	m := &Keymap{spec: s, tables: map[string][]Row{}}
	for name, p := range raw {
		switch {
		case name == helpTable:
			err = md.PrimitiveDecode(p, &m.help)
		case slices.Contains(s.Tables, name):
			var rows []Row
			err = md.PrimitiveDecode(p, &rows)
			for i := range rows {
				rows[i].Keys = keyNames(rows[i].Keys)
			}
			m.tables[name] = rows
		default:
			err = errors.New("unknown table " + name)
		}
		if err != nil {
			return nil, err
		}
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, errors.New("unknown " + u[0].String())
	}
	return m, nil
}

// Rows returns the rows of table.
func (m *Keymap) Rows(table string) []Row { return slices.Clone(m.tables[table]) }

// Tables returns the tables of Spec.Tables, in their order: a dialog that
// lists all rows goes through them.
func (m *Keymap) Tables() []string { return slices.Clone(m.spec.Tables) }

// TwoWay reports whether action is one of Spec.TwoWay, whose rows take two
// keys, one for each way.
func (m *Keymap) TwoWay(action string) bool { return slices.Contains(m.spec.TwoWay, action) }

// Row returns the row that gives the keys and words of action: the first in
// the first table of Spec.Tables that has it. Without one, it has no keys.
func (m *Keymap) Row(action string) Row {
	if rows := m.RowsOf(action); len(rows) > 0 {
		return rows[0]
	}
	return Row{}
}

// RowsOf returns the rows of action in the first table that has it, in the
// order of Row.
func (m *Keymap) RowsOf(action string) []Row {
	for _, t := range m.spec.Tables {
		if of := m.RowsIn(t, action); len(of) > 0 {
			return of
		}
	}
	return nil
}

// RowsIn returns the rows of action in table, nil when it has none: a widget
// takes its keys from the table its application names.
func (m *Keymap) RowsIn(table, action string) []Row {
	var of []Row
	for _, r := range m.tables[table] {
		if r.Action == action {
			of = append(of, r)
		}
	}
	return of
}

// RowsAt returns the rows of action in table, in the first table that has
// it when table is empty.
func (m *Keymap) RowsAt(table, action string) []Row {
	if table == "" {
		return m.RowsOf(action)
	}
	return m.RowsIn(table, action)
}

// RowsOn returns the rows of action in the first table of page that has it,
// the tables Key looks keys up in; nil when none has it: a popup shows the
// keys of its page.
func (m *Keymap) RowsOn(page, action string) []Row {
	for _, t := range m.pageTables(page) {
		if of := m.RowsIn(t, action); len(of) > 0 {
			return of
		}
	}
	return nil
}

// name is a name of a table or an action.
const name = `[a-z0-9_]+`

// isName matches a name.
var isName = regexp.MustCompile(`^` + name + `$`)

// placeholder matches {action}, {action:n}, {table.action} and
// {table.action:n} in the words of a file.
var placeholder = regexp.MustCompile(`\{(?:(` + name + `)\.)?(` + name + `)(?::([0-9]+))?\}`)

// placeheld returns the row of the placeholder sub matched, false when it
// does not exist.
func (m *Keymap) placeheld(sub []string) (Row, bool) {
	n, _ := strconv.Atoi(sub[3])
	if rows := m.RowsAt(sub[1], sub[2]); n < len(rows) {
		return rows[n], true
	}
	return Row{}, false
}

// Text returns s with each placeholder replaced by the keys of its row, ""
// for a row that does not exist.
func (m *Keymap) Text(s string) string {
	return m.Replace(s, func(r Row) string { return strings.Join(r.Keys, " ") })
}

// Replace returns s with each placeholder replaced by its row as show shows
// it, "" for a row that does not exist: a help shows the keys otherwise than
// the file names them.
func (m *Keymap) Replace(s string, show func(Row) string) string {
	return placeholder.ReplaceAllStringFunc(s, func(match string) string {
		if r, ok := m.placeheld(placeholder.FindStringSubmatch(match)); ok {
			return show(r)
		}
		return ""
	})
}

// Hit is what a key does: the action of its row, "" for none, and its place
// in the row's keys, which gives the way of a two-way action. Letter is set
// for a key written with <letter> after it (m<letter>): the application reads
// the letter typed next.
type Hit struct {
	Action string
	I      int
	Letter bool
}

// keyLetter ends a key after which the application reads a letter typed next,
// as m<letter>; only at the end, so that < and ctrl+< are keys of their own.
const keyLetter = "<letter>"

// KeyPress returns the key press of a key as a file writes it, without
// <letter>, and whether a letter follows it: what a binding matches.
func KeyPress(name string) (string, bool) { return strings.CutSuffix(name, keyLetter) }

// pageTables returns the tables the keys of page are looked up in.
func (m *Keymap) pageTables(page string) []string {
	if tables, ok := m.spec.Pages[page]; ok {
		return tables
	}
	return []string{page}
}

// Key returns what key k, named as a key press names it, does on page: the
// first row with it in the tables of the page decides.
func (m *Keymap) Key(page, k string) Hit {
	for _, t := range m.pageTables(page) {
		for _, r := range m.tables[t] {
			for i, name := range r.Keys {
				if p, letter := KeyPress(name); p == k {
					return Hit{r.Action, i, letter}
				}
			}
		}
	}
	return Hit{}
}

// FirstKeys returns the first key of each row of table that has keys.
func (m *Keymap) FirstKeys(table string) []string {
	var keys []string
	for _, r := range m.tables[table] {
		if len(r.Keys) > 0 {
			keys = append(keys, r.Keys[0])
		}
	}
	return keys
}

// Line is a line of the help: the line of the table help, the keys of its
// action, nil for a heading or a note, and its words with the placeholders
// replaced: the heading, the note or the help of the action.
type Line struct {
	HelpLine
	Keys []string
	Text string
}

// Lines returns the lines of the help.
func (m *Keymap) Lines() []Line {
	var lines []Line
	for _, h := range m.help {
		l := Line{HelpLine: h}
		switch {
		case h.Heading != "":
			l.Text = h.Heading
		case h.Note != "":
			l.Text = m.Text(h.Note)
		default:
			var r Row
			if rows := m.RowsAt(h.Table, h.Action); len(rows) > 0 {
				r = rows[0]
			}
			l.Keys, l.Text = r.Keys, m.Text(r.Help)
		}
		lines = append(lines, l)
	}
	return lines
}

// userRow is a row of a user's file, which changes only keys.
type userRow struct {
	Action string
	Keys   []string
}

// WithUser returns m with the keys of text, a user's file, and what is wrong
// with it. The rows of an action in a table replace all its rows there,
// keeping their words but not their display, which shows the keys replaced;
// a file with a problem changes nothing, so that a mistake cannot take away
// the keys to quit or get help, also not a row without keys.
func (m *Keymap) WithUser(text string) (*Keymap, []string) {
	var u map[string][]userRow
	md, err := toml.Decode(text, &u)
	if err != nil {
		return m, []string{err.Error()}
	}
	for _, rows := range u {
		for i := range rows {
			rows[i].Keys = keyNames(rows[i].Keys)
		}
	}
	var problems []string
	for _, k := range md.Undecoded() {
		problems = append(problems, "unknown "+k.String())
	}
	for _, name := range slices.Sorted(maps.Keys(u)) {
		if !slices.Contains(m.spec.Tables, name) {
			problems = append(problems, "unknown "+name)
		}
	}
	merged := &Keymap{spec: m.spec, tables: maps.Clone(m.tables), help: m.help}
	for _, t := range m.spec.Tables {
		rows := m.tables[t]
		for _, r := range u[t] {
			switch {
			case !slices.ContainsFunc(rows, func(b Row) bool { return b.Action == r.Action }):
				problems = append(problems, "no action "+r.Action+" in "+t)
			case len(r.Keys) == 0:
				problems = append(problems, r.Action+" in "+t+" has no keys")
			case len(noKeyPresses(t, r.Keys)) > 0:
				problems = append(problems, noKeyPresses(t, r.Keys)...)
			case slices.Contains(m.spec.TwoWay, r.Action) && len(r.Keys) != 2:
				problems = append(problems, r.Action+" in "+t+" takes two keys, one each way")
			}
		}
		if len(u[t]) > 0 {
			merged.tables[t] = replaceKeys(rows, u[t])
		}
	}
	problems = append(problems, merged.Conflicts()...)
	if len(problems) > 0 {
		return m, problems
	}
	return merged, nil
}

// replaceKeys returns rows with the rows of each action of user in place of
// its rows: the first where the first was, with the words of the row it
// replaces, without its display; rows of user beyond them have none.
func replaceKeys(rows []Row, user []userRow) []Row {
	var out []Row
	done := map[string]bool{}
	for _, r := range rows {
		if !slices.ContainsFunc(user, func(u userRow) bool { return u.Action == r.Action }) {
			out = append(out, r)
			continue
		}
		if done[r.Action] {
			continue
		}
		done[r.Action] = true
		var old []Row
		for _, o := range rows {
			if o.Action == r.Action {
				old = append(old, o)
			}
		}
		i := 0
		for _, u := range user {
			if u.Action != r.Action {
				continue
			}
			row := Row{Action: u.Action}
			if i < len(old) {
				row = old[i]
			}
			row.Keys, row.Display = u.Keys, ""
			out = append(out, row)
			i++
		}
	}
	return out
}

// Load returns m with the keys of the user's file at path, as WithUser; a
// file that does not exist is no problem and changes nothing.
func (m *Keymap) Load(path string) (*Keymap, []string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, []string{err.Error()}
	}
	return m.WithUser(string(data))
}

// Conflicts returns the keys with two actions in one table.
func (m *Keymap) Conflicts() []string {
	var problems []string
	for _, t := range m.spec.Tables {
		actions := map[string]string{}
		for _, r := range m.tables[t] {
			for _, k := range r.Keys {
				k, _ = KeyPress(k) // m<letter> and m are one key press
				if a, ok := actions[k]; ok && a != r.Action {
					problems = append(problems, "key "+k+" has two actions in "+t+": "+a+", "+r.Action)
				}
				actions[k] = r.Action
			}
		}
	}
	return problems
}

// tablesOf returns the tables with a row of action, in the order of
// Spec.Tables.
func (m *Keymap) tablesOf(action string) []string {
	var of []string
	for _, t := range m.spec.Tables {
		if len(m.RowsIn(t, action)) > 0 {
			of = append(of, t)
		}
	}
	return of
}

// Problems returns what is wrong with m: the Conflicts, a table or an action
// whose name a placeholder cannot take, a line of the help
// with an action that has no help, a placeholder without its row, a line of
// the help or a placeholder without a table for an action in several tables,
// whose keys a user's file may make differ. An application checks its own
// file with it in a test, and adds its own checks.
func (m *Keymap) Problems() []string {
	problems := m.Conflicts()
	for _, t := range m.spec.Tables {
		for _, r := range m.tables[t] {
			problems = append(problems, noKeyPresses(t, r.Keys)...)
		}
	}
	named := map[string]bool{}
	for _, t := range m.spec.Tables {
		if !isName.MatchString(t) {
			problems = append(problems, "table "+t+": a name is a-z, 0-9 and _")
		}
		for _, r := range m.tables[t] {
			if !named[r.Action] && !isName.MatchString(r.Action) {
				problems = append(problems, "action "+r.Action+": a name is a-z, 0-9 and _")
			}
			named[r.Action] = true
		}
	}
	for _, h := range m.help {
		if h.Action == "" {
			continue
		}
		if of := m.tablesOf(h.Action); h.Table == "" && len(of) > 1 {
			problems = append(problems, "the help's action "+h.Action+" is in several tables: "+strings.Join(of, ", "))
		}
		if rows := m.RowsAt(h.Table, h.Action); len(rows) == 0 || rows[0].Help == "" {
			name := h.Action
			if h.Table != "" {
				name = h.Table + "." + name
			}
			problems = append(problems, "the help's action "+name+" has no help")
		}
	}
	var words []string
	for _, t := range m.spec.Tables {
		for _, r := range m.tables[t] {
			words = append(words, r.Help, r.Setting)
		}
	}
	for _, h := range m.help {
		words = append(words, h.Note)
	}
	for _, w := range words {
		for _, sub := range placeholder.FindAllStringSubmatch(w, -1) {
			if _, ok := m.placeheld(sub); !ok {
				problems = append(problems, "placeholder "+sub[0]+" has no row")
			}
			if of := m.tablesOf(sub[2]); sub[1] == "" && len(of) > 1 {
				problems = append(problems, "placeholder "+sub[0]+" is in several tables: "+strings.Join(of, ", "))
			}
		}
	}
	return problems
}

// keyNames returns keys with the old spellings pgdn and F1 … as Bubble Tea
// names them, pgdown and f1 …, also after modifiers; the rest as written.
func keyNames(keys []string) []string {
	out := slices.Clone(keys)
	for i, k := range out {
		k, letter := KeyPress(k)
		mods, base := splitMods(k)
		switch {
		case base == "pgdn":
			base = "pgdown"
		case len(base) > 1 && base[0] == 'F' && strings.Trim(base[1:], "0123456789") == "":
			base = "f" + base[1:]
		}
		out[i] = mods + base
		if letter {
			out[i] += keyLetter
		}
	}
	return out
}

// splitMods returns the modifiers of key k with their last +, and the key
// itself; a key + stays whole (ctrl++).
func splitMods(k string) (mods, base string) {
	cut := strings.LastIndex(k[:max(len(k)-1, 0)], "+") + 1
	return k[:cut], k[cut:]
}

// modifiers are those a key press names before its key, joined by +.
var modifiers = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// keyWord matches a key a key press names with a word: pgdown, f12, space, kp5.
var keyWord = regexp.MustCompile(`^[a-z][a-z0-9_]+$`)

// noKeyPresses returns a problem for each of keys, those of a row of table,
// that cannot be a single key press, so that it would never match: one
// character or a word of a-z, 0-9 and _ (Bubble Tea's names), after known
// modifiers, then <letter> or nothing.
func noKeyPresses(table string, keys []string) []string {
	var problems []string
	for _, name := range keys {
		k, _ := KeyPress(name)
		mods, base := splitMods(k)
		ok := utf8.RuneCountInString(base) == 1 || keyWord.MatchString(base)
		for _, mod := range strings.Split(strings.TrimSuffix(mods, "+"), "+") {
			ok = ok && (mods == "" || slices.Contains(modifiers, mod))
		}
		if !ok {
			problems = append(problems, "key "+name+" in "+table+" is no key press")
		}
	}
	return problems
}
