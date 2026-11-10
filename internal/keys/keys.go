// Package keys holds the keymap of jdiff: keybindings.toml, which binds the
// keys of all pages to actions, gives their words and decides the order of
// the settings, with the keys of the user's keybindings.toml over it. A Map
// is built once, with the configuration, and not changed after.
package keys

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// BuiltinText is the built-in keybindings.toml.
//
//go:embed keybindings.toml
var BuiltinText string

// Row is a row of keybindings.toml.
type Row struct {
	Action  string
	Keys    []string
	Label   []string
	Help    string
	Setting string
}

// HelpRow is a line of the help popup in keybindings.toml.
type HelpRow struct{ Heading, Note, Action string }

// Map holds keybindings.toml: the rows of each page and the help's lines.
type Map struct {
	CommitList []Row     `toml:"commit_list"`
	FileList   []Row     `toml:"file_list"`
	DiffView   []Row     `toml:"diff_view"`
	FlowView   []Row     `toml:"flow_view"`
	Everywhere []Row     `toml:"everywhere"`
	DocView    []Row     `toml:"doc_view"`
	Help       []HelpRow `toml:"help"`
}

// page is a page of keybindings.toml: its name and its rows.
type page struct {
	name string
	rows *[]Row
}

// pages returns the pages of km in the order of keybindings.toml, which
// decides which page's row Row and RowsOf return.
func (km *Map) pages() []page {
	return []page{
		{"everywhere", &km.Everywhere}, {"diff_view", &km.DiffView}, {"flow_view", &km.FlowView},
		{"commit_list", &km.CommitList}, {"file_list", &km.FileList}, {"doc_view", &km.DocView},
	}
}

// Parse returns the Map of text, a keybindings.toml.
func Parse(text string) (Map, error) {
	var p Map
	_, err := toml.Decode(text, &p)
	return p, err
}

// builtin is BuiltinText, read. It is part of the program, so that failing to
// read it is a bug, which TestKeybindingsFile finds.
var builtin = func() Map {
	p, err := Parse(BuiltinText)
	if err != nil {
		panic(err)
	}
	return p
}()

// Builtin returns the built-in keys. They are shared: do not change them.
func Builtin() *Map { return &builtin }

// Row returns the row that gives the keys and words of action; see
// keybindings.toml for the order. Without one, it has no keys.
func (km Map) Row(action string) Row {
	if rows := km.RowsOf(action); len(rows) > 0 {
		return rows[0]
	}
	return Row{}
}

// RowsOf returns the rows of action in the first page that has it, in the
// order of Row.
func (km Map) RowsOf(action string) []Row {
	for _, pg := range km.pages() {
		var of []Row
		for _, r := range *pg.rows {
			if r.Action == action {
				of = append(of, r)
			}
		}
		if len(of) > 0 {
			return of
		}
	}
	return nil
}

// placeholder matches {action} and {action:n} in the words of keybindings.toml.
var placeholder = regexp.MustCompile(`\{([a-z_]+)(?::([0-9]+))?\}`)

// Text returns s with each placeholder replaced by the keys of its row, ""
// for a row that does not exist.
func (km Map) Text(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sub[2])
		if rows := km.RowsOf(sub[1]); n < len(rows) {
			return strings.Join(rows[n].Keys, " ")
		}
		return ""
	})
}

// twoWay lists the actions whose rows take two keys, one for each way.
var twoWay = []string{"move", "page", "half_page", "ends", "scroll", "change", "next_file", "history", "match", "link", "line_link", "next_page"}

// userRow is a row of the user's keybindings.toml, which changes only keys.
type userRow struct {
	Action string
	Keys   []string
}

// ApplyUserKeys returns km with the keys of text, the user's
// keybindings.toml, applied over them, and what is wrong with it. The rows of
// an action on a page replace all its rows there, keeping their words; a file
// with a problem changes nothing, so that a mistake cannot take away the keys
// to quit or get help.
func (km Map) ApplyUserKeys(text string) (Map, []string) {
	var u struct {
		CommitList []userRow `toml:"commit_list"`
		FileList   []userRow `toml:"file_list"`
		DiffView   []userRow `toml:"diff_view"`
		FlowView   []userRow `toml:"flow_view"`
		Everywhere []userRow `toml:"everywhere"`
		DocView    []userRow `toml:"doc_view"`
	}
	md, err := toml.Decode(text, &u)
	if err != nil {
		return km, []string{err.Error()}
	}
	var problems []string
	for _, k := range md.Undecoded() {
		problems = append(problems, "unknown "+k.String())
	}
	merged := km
	fromFile := map[string][]userRow{
		"everywhere": u.Everywhere, "diff_view": u.DiffView, "flow_view": u.FlowView,
		"commit_list": u.CommitList, "file_list": u.FileList, "doc_view": u.DocView,
	}
	for _, pg := range merged.pages() {
		for _, r := range fromFile[pg.name] {
			switch {
			case !slices.ContainsFunc(*pg.rows, func(b Row) bool { return b.Action == r.Action }):
				problems = append(problems, "no action "+r.Action+" in "+pg.name)
			case len(r.Keys) == 0:
				problems = append(problems, r.Action+" in "+pg.name+" has no keys")
			case slices.Contains(twoWay, r.Action) && len(r.Keys) != 2:
				problems = append(problems, r.Action+" in "+pg.name+" takes two keys, one each way")
			}
		}
		*pg.rows = replaceKeys(*pg.rows, fromFile[pg.name])
	}
	problems = append(problems, merged.conflicts()...)
	if len(problems) > 0 {
		return km, problems
	}
	return merged, nil
}

// replaceKeys returns rows with the rows of each action of user in place of
// its rows: the first where the first was, with the words of the row it
// replaces; rows of user beyond them have none.
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
			row.Keys = u.Keys
			out = append(out, row)
			i++
		}
	}
	return out
}

// FileName is the user's keybindings.toml in the configuration directory.
const FileName = "keybindings.toml"

// Load returns the built-in keys with those of the user's keybindings.toml in
// the configuration directory dir, if there is one, and what is wrong with
// it; with a problem, the built-in keys.
func Load(dir string) (*Map, []string) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Builtin(), nil
	}
	if err != nil {
		return Builtin(), []string{err.Error()}
	}
	p, problems := Builtin().ApplyUserKeys(string(data))
	if len(problems) > 0 {
		return Builtin(), problems
	}
	return &p, nil
}

// Page is a page whose keys are looked up in keybindings.toml.
type Page int

const (
	CommitList Page = iota
	FileList
	DiffView
	FlowView
	DocView
	Everywhere // the popups over all pages
)

// Hit is what a key does: the action of its row, "" for none, and its index
// in the row, which gives the way of actions that go two ways (see
// keybindings.toml).
type Hit struct {
	Action string
	Index  int
}

// tables returns the rows the keys of page pg are looked up in, in order.
func (km Map) tables(pg Page) [][]Row {
	switch pg {
	case CommitList:
		return [][]Row{km.CommitList, km.Everywhere}
	case FileList:
		return [][]Row{km.FileList, km.Everywhere}
	case DiffView:
		return [][]Row{km.DiffView, km.Everywhere}
	case FlowView:
		return [][]Row{km.FlowView, km.DiffView, km.Everywhere}
	case DocView:
		return [][]Row{km.DocView, km.Everywhere}
	}
	return [][]Row{km.Everywhere}
}

// Key returns what key k, as Bubble Tea names it, does on page pg.
func (km Map) Key(pg Page, k string) Hit {
	for _, rows := range km.tables(pg) {
		for _, r := range rows {
			for i, name := range r.Keys {
				if Name(name) == k {
					return Hit{r.Action, i}
				}
			}
		}
	}
	return Hit{}
}

// Name returns Bubble Tea's name of the key written as name in
// keybindings.toml, which writes pgdn and F5 as the help shows them, and
// <letter> for the letter that follows.
func Name(name string) string {
	name = strings.TrimSuffix(name, "<letter>")
	if name == "pgdn" {
		return "pgdown"
	}
	if n, ok := strings.CutPrefix(name, "F"); ok && n != "" && strings.Trim(n, "0123456789") == "" {
		return "f" + n
	}
	return name
}

// LabelIn returns the word of the status bar in state, "" when there is none.
func (r Row) LabelIn(state int) string {
	if state < len(r.Label) {
		return r.Label[state]
	}
	return ""
}

// Order returns the first key of each row of everywhere, which decides the
// order of the settings.
func (km Map) Order() []string {
	var keys []string
	for _, r := range km.Everywhere {
		if len(r.Keys) > 0 {
			keys = append(keys, r.Keys[0])
		}
	}
	return keys
}

// Problems returns what is wrong with km by itself: a key with two actions on
// one page, a line of the help with an action that has no help, a placeholder
// without its row. What the settings and the help need of it is checked by
// the package ui.
func (km Map) Problems() []string {
	problems := km.conflicts()
	for _, r := range km.Help {
		if r.Action != "" && km.Row(r.Action).Help == "" {
			problems = append(problems, "the help's action "+r.Action+" has no help")
		}
	}
	var words []string
	for _, pg := range km.pages() {
		for _, r := range *pg.rows {
			words = append(words, r.Help, r.Setting)
		}
	}
	for _, r := range km.Help {
		words = append(words, r.Note)
	}
	for _, w := range words {
		for _, m := range placeholder.FindAllStringSubmatch(w, -1) {
			n, _ := strconv.Atoi(m[2])
			if n >= len(km.RowsOf(m[1])) {
				problems = append(problems, "placeholder "+m[0]+" has no row")
			}
		}
	}
	return problems
}

// conflicts returns the keys with two actions on one page.
func (km Map) conflicts() []string {
	var problems []string
	for _, pg := range km.pages() {
		actions := map[string]string{}
		for _, r := range *pg.rows {
			for _, name := range r.Keys {
				if a, ok := actions[Name(name)]; ok && a != r.Action {
					problems = append(problems, "key "+name+" has two actions in "+pg.name+": "+a+", "+r.Action)
				}
				actions[Name(name)] = r.Action
			}
		}
	}
	return problems
}
