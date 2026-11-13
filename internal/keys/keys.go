// Package keys holds the keymap of jdiff: keybindings.toml, which binds the
// keys of all pages to actions, gives their words and decides the order of
// the settings, with the keys of the user's keybindings.toml over it, read by
// keymap with jdiff's Spec. A Map is built once, with the configuration, and
// not changed after; changing keys in the settings popup builds a new one
// (config.SetKeys).
package keys

import (
	_ "embed"
	"strings"

	"jdiff/internal/keymap"
)

// BuiltinText is the built-in keybindings.toml.
//
//go:embed keybindings.toml
var BuiltinText string

// The pages of keybindings.toml, each a table of its own.
const (
	CommitList = "commit_list"
	FileList   = "file_list"
	DiffView   = "diff_view"
	FlowView   = "flow_view"
	DocView    = "doc_view"
	Everywhere = "everywhere" // the popups over all pages
)

// twoWay lists the actions whose rows take two keys, one for each way.
var twoWay = []string{"move", "page", "half_page", "ends", "scroll", "change", "next_file", "history", "match", "link", "line_link", "next_page"}

// Spec is keybindings.toml: its pages in the order an action is looked up,
// the pages each page looks keys up in, and the actions that go two ways.
var Spec = keymap.Spec{
	Tables: []string{Everywhere, DiffView, FlowView, CommitList, FileList, DocView},
	Pages: map[string][]string{
		CommitList: {CommitList, Everywhere},
		FileList:   {FileList, Everywhere},
		DiffView:   {DiffView, Everywhere},
		FlowView:   {FlowView, DiffView, Everywhere},
		DocView:    {DocView, Everywhere},
	},
	TwoWay: twoWay,
}

// Map is a keymap read with Spec, whose words show the keys as Shown does.
type Map struct{ *keymap.Keymap }

// Text returns s with each placeholder replaced by the keys of its row, as
// Shown shows them, "" for a row that does not exist.
func (km *Map) Text(s string) string {
	return km.Replace(s, func(r keymap.Row) string { return strings.Join(Shown(r.Keys), " ") })
}

// Parse returns the Map of text, a keybindings.toml.
func Parse(text string) (*Map, error) {
	km, err := keymap.Parse(text, Spec)
	if err != nil {
		return nil, err
	}
	return &Map{km}, nil
}

// builtin is BuiltinText, read. It is part of the program, so that failing to
// read it is a bug, which TestKeybindingsFile finds.
var builtin = func() *Map {
	km, err := Parse(BuiltinText)
	if err != nil {
		panic(err)
	}
	return km
}()

// Builtin returns the built-in keys. They are shared: do not change them.
func Builtin() *Map { return builtin }

// FileName is the user's keybindings.toml in the configuration directory.
const FileName = "keybindings.toml"

// Load returns the built-in keys with those of the user's keybindings.toml at
// path, if there is one, and what is wrong with it; with a problem, the
// built-in keys.
func Load(path string) (*Map, []string) {
	km, problems := builtin.Load(path)
	if km == builtin.Keymap {
		return builtin, problems
	}
	return &Map{km}, problems
}

// Spelling returns the name keybindings.toml writes for Bubble Tea's key k,
// as the help shows it: pgdn for pgdown, F5 for f5, also after modifiers; the
// rest, <letter> too, as it is.
func Spelling(k string) string {
	k, letter := keymap.KeyPress(k)
	mods, base := "", k
	if i := strings.LastIndexByte(k, '+'); i >= 0 && i < len(k)-1 {
		mods, base = k[:i+1], k[i+1:]
	}
	switch {
	case base == "pgdown":
		base = "pgdn"
	case len(base) > 1 && base[0] == 'f' && strings.Trim(base[1:], "0123456789") == "":
		base = "F" + base[1:]
	}
	if letter {
		return mods + base + "<letter>"
	}
	return mods + base
}

// Shown returns keys as the help and the status bars show them (Spelling).
func Shown(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = Spelling(k)
	}
	return out
}
