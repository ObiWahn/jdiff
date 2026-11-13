// Package teakeys turns the rows of a keymap into the key.Binding of Bubble
// Tea's bubbles, so that a widget with a KeyMap of bindings takes its keys
// from the table its application names. It is a module of its own, so that
// keymap does not depend on Bubble Tea.
//
// The file names keys as Bubble Tea does (tea.KeyPressMsg.String(): pgdown,
// f1, ctrl+pgdown); a binding takes them as written. Display only changes how
// a help shows them.
package teakeys

import (
	"strings"

	"charm.land/bubbles/v2/key"

	"jdiff/internal/keymap"
)

// displayBase are the keys a help shows otherwise than a file writes them.
var displayBase = map[string]string{
	"up": "↑", "down": "↓", "left": "←", "right": "→", "enter": "↵", "space": "spc", "pgdown": "pgdn",
	"delete": "del",
}

// split returns the modifiers of a key written as name, with their last +,
// and the key itself; the key + stays whole.
func split(name string) (mods, base string) {
	if i := strings.LastIndex(name[:max(len(name)-1, 0)], "+"); i >= 0 {
		return name[:i+1], name[i+1:]
	}
	return "", name
}

// Display returns a key written as name in a file as a help shows it: the
// arrows ↑↓←→, enter ↵, space spc, pgdown pgdn, delete del, f1 F1; the rest stays,
// shift too (shift+↑, not ⇧↑), and <letter> after the key (m<letter>).
func Display(name string) string {
	k, letter := keymap.KeyPress(name)
	mods, base := split(k)
	if d, ok := displayBase[base]; ok {
		base = d
	} else if n, ok := strings.CutPrefix(base, "f"); ok && n != "" && strings.Trim(n, "0123456789") == "" {
		base = "F" + n
	}
	if letter {
		return mods + base + strings.TrimPrefix(name, k)
	}
	return mods + base
}

// shown returns how a help shows the keys of r: its display, else keys as
// gen makes it.
func shown(r keymap.Row, gen func([]string) string) string {
	if r.Display != "" {
		return r.Display
	}
	return gen(r.Keys)
}

// Binding returns the keys of all rows of action in table as one binding;
// its help shows the first row: its display, else its keys (Display, joined
// with /), and its label, none without one. fallback when the table has no
// row of action. Rows without keys give a binding without keys, which does
// nothing.
func Binding(km *keymap.Keymap, table, action string, fallback key.Binding) key.Binding {
	return binding(km.RowsIn(table, action), fallback)
}

// BindingOn is Binding on the rows of action on page (keymap.RowsOn): in the
// first table of the page that has it.
func BindingOn(km *keymap.Keymap, page, action string, fallback key.Binding) key.Binding {
	return binding(km.RowsOn(page, action), fallback)
}

// binding returns the keys of rows as one binding, as Binding.
func binding(rows []keymap.Row, fallback key.Binding) key.Binding {
	if len(rows) == 0 {
		return fallback
	}
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.Keys...)
	}
	return bind(keys, shown(rows[0], displayAll), rows[0].LabelOf(0))
}

// displayAll returns keys as a help shows them, joined with /.
func displayAll(keys []string) string {
	shown := make([]string, len(keys))
	for i, k := range keys {
		shown[i] = Display(k)
	}
	return strings.Join(shown, "/")
}

// pairs are the arrow pairs of a two-way row, forward first, and how a help
// shows them.
var pairs = map[[2]string]string{{"down", "up"}: "↑↓", {"right", "left"}: "←→"}

// twoWayHelp returns the help of the keys of a two-way row, the same for both
// ways: an arrow pair with the same modifiers as those and ↑↓ or ←→, other
// keys as Binding shows them.
func twoWayHelp(keys []string) string {
	if len(keys) == 2 {
		m0, b0 := split(keys[0])
		m1, b1 := split(keys[1])
		if p, ok := pairs[[2]string{b0, b1}]; ok && m0 == m1 {
			return m0 + p
		}
	}
	return displayAll(keys)
}

// TwoWay returns the bindings of an action that goes two ways: the first key
// of each row goes forward, the second back; the fallbacks when the table has
// no row of action. Both have the help of the first row: its display, else
// twoWayHelp of its keys, and its label.
func TwoWay(km *keymap.Keymap, table, action string, fwd, back key.Binding) (key.Binding, key.Binding) {
	return twoWay(km.RowsIn(table, action), fwd, back)
}

// TwoWayOn is TwoWay on the rows of action on page (keymap.RowsOn): in the
// first table of the page that has it.
func TwoWayOn(km *keymap.Keymap, page, action string, fwd, back key.Binding) (key.Binding, key.Binding) {
	return twoWay(km.RowsOn(page, action), fwd, back)
}

// twoWay returns the bindings of rows of a two-way action, as TwoWay.
func twoWay(rows []keymap.Row, fwd, back key.Binding) (key.Binding, key.Binding) {
	if len(rows) == 0 {
		return fwd, back
	}
	help := shown(rows[0], twoWayHelp)
	way := func(i int) key.Binding {
		var keys []string
		for _, r := range rows {
			if i < len(r.Keys) {
				keys = append(keys, r.Keys[i])
			}
		}
		return bind(keys, help, rows[0].LabelOf(0))
	}
	return way(0), way(1)
}

// Shown returns how a hint shows the keys of action in table (the first table
// that has it when table is empty): the first row's display, else its keys
// as twoWayHelp shows them; "" without a row.
func Shown(km *keymap.Keymap, table, action string) string {
	if rows := km.RowsAt(table, action); len(rows) > 0 {
		return shown(rows[0], twoWayHelp)
	}
	return ""
}

// Text returns s with each placeholder of keymap ({action}, {table.action},
// with :n) replaced by the keys of its row as Shown shows them.
func Text(km *keymap.Keymap, s string) string {
	return km.Replace(s, func(r keymap.Row) string { return shown(r, twoWayHelp) })
}

// bind returns a binding of keys, as the file names them, with help; a key
// with <letter> binds the key before it (keymap.KeyPress).
func bind(keys []string, help, desc string) key.Binding {
	var presses []string
	for _, k := range keys {
		p, _ := keymap.KeyPress(k)
		presses = append(presses, p)
	}
	return key.NewBinding(key.WithKeys(presses...), key.WithHelp(help, desc))
}
