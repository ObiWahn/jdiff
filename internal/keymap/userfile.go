package keymap

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// decodeUser returns the rows of text, a user's file, by table.
func decodeUser(text string) (map[string][]userRow, error) {
	var u map[string][]userRow
	_, err := toml.Decode(text, &u)
	return u, err
}

// SetUserRows returns text, a user's file, with rows, the keys of each row,
// as the rows of action in table; no rows take them out, so that the
// built-in ones apply. Only those rows are written anew, where the first of
// them stood, in the form of the file: inline tables in name = [...], or
// TOML's array of tables [[name]]; the rest stays as written, also comments,
// rows over several lines and odd spacing. What it returns is checked to hold
// the rows of text with only that change, else it is an error. It does not
// check the keys themselves: SaveUser does, with WithUser.
func SetUserRows(text, table, action string, rows [][]string) (string, error) {
	old, err := decodeUser(text)
	if err != nil {
		return "", err
	}
	l, err := scanLayout(text, table)
	if err != nil {
		return "", err
	}
	var edits []edit
	switch {
	case l.hasArray:
		edits, err = l.arrayEdits(text, table, action, rows)
	case len(l.tables) > 0:
		edits, err = l.tableEdits(text, table, action, rows)
	case len(rows) == 0:
	case l.headers: // a name = [...] would belong to the TOML table above
		edits = []edit{{len(text), len(text), endLine(text) + "\n" + tablesText(table, action, rows)}}
	default:
		var b strings.Builder
		b.WriteString(endLine(text) + table + " = [\n")
		for _, keys := range rows {
			b.WriteString("    " + inlineRow(action, keys) + ",\n")
		}
		edits = []edit{{len(text), len(text), b.String() + "]\n"}}
	}
	if err != nil {
		return "", err
	}
	// From the end, so that each edit finds its place; of two at one place
	// the later in edits goes first in out.
	out := text
	slices.SortStableFunc(edits, func(a, b edit) int { return b.from - a.from })
	for _, e := range edits {
		out = out[:e.from] + e.text + out[e.to:]
	}
	if !changedOnly(old, out, table, action, rows) {
		return "", errors.New("cannot change " + action + " in " + table + " of the user's file in place: edit it by hand")
	}
	return out, nil
}

// SaveUser makes rows, the keys of each row, the rows of action in table of
// the user's file at path, nil the built-in ones, and returns m with the keys
// of the file, as Load: m is the application's own file, read with Parse. A
// file that would have a problem, as a key with two actions in one table, is
// not written and m comes back: the error says what is wrong. The rest of the
// file stays as written, see SetUserRows.
func (m *Keymap) SaveUser(path, table, action string, rows [][]string) (*Keymap, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, err
	}
	text, err := SetUserRows(string(data), table, action, rows)
	if err != nil {
		return m, err
	}
	merged, problems := m.WithUser(text)
	if len(problems) > 0 {
		return m, errors.New(strings.Join(problems, "; "))
	}
	if err := writeFile(path, []byte(text)); err != nil {
		return m, err
	}
	return merged, nil
}

// ResetUser moves the user's file at path to path.bak, over an older one, so
// that what was written by hand is kept, and returns m, the application's own
// keys.
func (m *Keymap) ResetUser(path string) (*Keymap, error) {
	if err := os.Rename(path, path+".bak"); err != nil {
		return m, err
	}
	return m, nil
}

// writeFile writes data to path through a temporary file next to it, so that
// the file is never left half written; each write has a temporary file of
// its own, so that two programs writing at once do not rename each other's.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Close(), os.Chmod(tmp.Name(), 0o644))
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// changedOnly reports whether out holds the rows of old with those of action
// in table replaced by rows.
func changedOnly(old map[string][]userRow, out, table, action string, rows [][]string) bool {
	got, err := decodeUser(out)
	if err != nil {
		return false
	}
	if _, ok := old[table]; !ok {
		old = maps.Clone(old)
		if old == nil {
			old = map[string][]userRow{}
		}
		old[table] = nil
	}
	same := func(a, b userRow) bool { return a.Action == b.Action && slices.Equal(a.Keys, b.Keys) }
	other := func(r userRow) bool { return r.Action != action }
	for name := range got {
		if _, ok := old[name]; !ok && name != table {
			return false
		}
	}
	for name, was := range old {
		if name != table {
			if !slices.EqualFunc(got[name], was, same) {
				return false
			}
			continue
		}
		if !slices.EqualFunc(filter(got[name], other), filter(was, other), same) {
			return false
		}
		var want []userRow
		for _, keys := range rows {
			want = append(want, userRow{action, keys})
		}
		if !slices.EqualFunc(filter(got[name], func(r userRow) bool { return !other(r) }), want, same) {
			return false
		}
	}
	return true
}

func filter(rows []userRow, keep func(userRow) bool) []userRow {
	var out []userRow
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// edit replaces the bytes [from, to) of a text with text.
type edit struct {
	from, to int
	text     string
}

// span is the bytes [from, to) of a text.
type span struct{ from, to int }

// layout is where the rows of a table stand in a user's file.
type layout struct {
	hasArray bool
	array    span   // the value of name = [...], with its brackets
	elements []span // its elements
	tables   []span // the [[name]] tables, from the header to the end of the line of their last value
	headers  bool   // whether the text has a TOML table header: no name = [...] can come after it
}

// arrayEdits returns the edits that make rows the rows of action in the
// array name = [...] of the table: the first of its elements takes them, the
// others go, each with its comma and, when it stood alone, its line. Without
// one, they go at the end of the array, in lines of their own.
func (l layout) arrayEdits(text, table, action string, rows [][]string) ([]edit, error) {
	var mine []span
	for _, el := range l.elements {
		a, err := actionOf(table+" = ["+text[el.from:el.to]+"]", table)
		if err != nil {
			return nil, err
		}
		if a == action {
			mine = append(mine, el)
		}
	}
	var texts []string
	for _, keys := range rows {
		texts = append(texts, inlineRow(action, keys))
	}
	var edits []edit
	if len(mine) > 0 {
		first := mine[0]
		if len(texts) > 0 {
			edits = append(edits, edit{first.from, first.to, strings.Join(texts, ",\n"+indentOf(text, first.from))})
			mine = mine[1:]
		}
		for _, el := range mine {
			edits = append(edits, removal(text, el))
		}
		return edits, nil
	}
	if len(texts) == 0 {
		return nil, nil
	}
	indent, comma := "    ", -1
	if n := len(l.elements); n > 0 {
		last := l.elements[n-1]
		if after := skipBlank(text, last.to); after < len(text) && text[after] != ',' {
			comma = last.to
		}
		if onlySpaceBefore(text, last.from) {
			indent = indentOf(text, last.from)
		}
	}
	var lines strings.Builder
	for _, t := range texts {
		lines.WriteString(indent + t + ",\n")
	}
	if end := l.array.to - 1; onlySpaceBefore(text, end) {
		edits = append(edits, edit{lineStart(text, end), lineStart(text, end), lines.String()})
	} else {
		edits = append(edits, edit{end, end, "\n" + lines.String()})
	}
	if comma >= 0 { // after the lines, which it goes before where both are at the same place
		edits = append(edits, edit{comma, comma, ","})
	}
	return edits, nil
}

// tableEdits returns the edits that make rows the rows of action in the
// [[name]] tables of the table: the first of them takes them, the others
// go. Without one, they go after its last [[name]].
func (l layout) tableEdits(text, table, action string, rows [][]string) ([]edit, error) {
	var mine []span
	for _, tb := range l.tables {
		a, err := actionOf(text[tb.from:tb.to], table)
		if err != nil {
			return nil, err
		}
		if a == action {
			mine = append(mine, tb)
		}
	}
	var edits []edit
	if len(mine) > 0 {
		first := mine[0]
		if len(rows) > 0 {
			edits = append(edits, edit{first.from, first.to, tablesText(table, action, rows)})
			mine = mine[1:]
		}
		for _, tb := range mine {
			edits = append(edits, edit{tb.from, tb.to, ""})
		}
		return edits, nil
	}
	if len(rows) == 0 {
		return nil, nil
	}
	end := l.tables[len(l.tables)-1].to
	return []edit{{end, end, endLine(text[:end]) + "\n" + tablesText(table, action, rows)}}, nil
}

// actionOf returns the action of the one row of table in text.
func actionOf(text, table string) (string, error) {
	rows, err := decodeUser(text)
	if err != nil || len(rows[table]) != 1 {
		return "", fmt.Errorf("keymap: cannot read the row %q of %s", strings.TrimSpace(text), table)
	}
	return rows[table][0].Action, nil
}

// removal returns the edit that takes the element el out of its array, with
// the comma after it, and its line when nothing else stands on it.
func removal(text string, el span) edit {
	from, to := el.from, el.to
	if j := skipSpace(text, to); j < len(text) && text[j] == ',' {
		to = j + 1
	}
	if onlySpaceBefore(text, from) {
		if j := skipSpace(text, to); j == len(text) || text[j] == '\n' || text[j] == '\r' {
			from, to = lineStart(text, from), lineEnd(text, to)
		}
	}
	return edit{from, to, ""}
}

// inlineRow returns the row of action with keys as an inline table, as the
// documentation writes it.
func inlineRow(action string, keys []string) string {
	return "{ action = " + quote(action) + ", keys = " + quoteList(keys) + " }"
}

// tablesText returns the rows of action with keys as [[name]] tables of table.
func tablesText(table, action string, rows [][]string) string {
	var tables []string
	for _, keys := range rows {
		tables = append(tables, "[["+table+"]]\naction = "+quote(action)+"\nkeys = "+quoteList(keys)+"\n")
	}
	return strings.Join(tables, "\n")
}

func quoteList(list []string) string {
	quoted := make([]string, len(list))
	for i, s := range list {
		quoted[i] = quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// quote returns s as a basic string of TOML. The names of keys are printable;
// changedOnly finds any that is not.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// endLine returns the newline that text, when not empty, lacks at its end.
func endLine(text string) string {
	if text == "" || strings.HasSuffix(text, "\n") {
		return ""
	}
	return "\n"
}

func lineStart(text string, i int) int { return strings.LastIndexByte(text[:i], '\n') + 1 }

// lineEnd returns where the line after i ends, past its newline.
func lineEnd(text string, i int) int {
	if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(text)
}

func onlySpaceBefore(text string, i int) bool {
	return strings.Trim(text[lineStart(text, i):i], " \t") == ""
}

// indentOf returns the spaces and tabs that begin the line of i.
func indentOf(text string, i int) string {
	line := text[lineStart(text, i):i]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func skipSpace(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return i
}

// skipBlank returns the first byte from i that is no space, newline or
// comment.
func skipBlank(text string, i int) int {
	for i < len(text) {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			i++
		case '#':
			i = lineEnd(text, i)
		default:
			return i
		}
	}
	return i
}

// scanLayout returns where the rows of table stand in text, which TOML has
// already read without error: the scanner knows only as much of TOML as it
// takes to find them.
func scanLayout(text, table string) (layout, error) {
	s := &scanner{text: text}
	var l layout
	header, inTable := "", false // the TOML table the text is in, and whether it is a [[table]]
	for {
		s.i = skipBlank(text, s.i)
		if s.i >= len(text) {
			return l, s.err
		}
		start := s.i
		if strings.HasPrefix(text[s.i:], "[") {
			double := strings.HasPrefix(text[s.i:], "[[")
			s.i += 1 + btoi(double)
			s.i = skipSpace(text, s.i)
			header = s.key()
			s.i = skipSpace(text, s.i)
			s.expect(strings.Repeat("]", 1+btoi(double)))
			l.headers, inTable = true, double && header == table
			if inTable {
				l.tables = append(l.tables, span{start, lineEnd(text, s.i)})
			}
			s.i = lineEnd(text, s.i)
		} else {
			name := s.key()
			s.i = skipSpace(text, s.i)
			s.expect("=")
			s.i = skipSpace(text, s.i)
			from := s.i
			s.value()
			if header == "" && name == table && strings.HasPrefix(text[from:], "[") {
				l.hasArray, l.array = true, span{from, s.i}
				l.elements = s.elements(from)
			}
			if inTable {
				l.tables[len(l.tables)-1].to = lineEnd(text, s.i)
			}
		}
		if s.err != nil {
			return l, s.err
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scanner reads a TOML text from i; the first problem stops it, in err.
type scanner struct {
	text string
	i    int
	err  error
}

func (s *scanner) fail(what string) {
	if s.err == nil {
		s.err = fmt.Errorf("keymap: line %d: %s", strings.Count(s.text[:s.i], "\n")+1, what)
	}
	s.i = len(s.text)
}

func (s *scanner) expect(t string) {
	if !strings.HasPrefix(s.text[s.i:], t) {
		s.fail("no " + t)
		return
	}
	s.i += len(t)
}

// key reads a key, dotted or quoted, and returns its parts joined by dots,
// those quoted as written between the quotes.
func (s *scanner) key() string {
	var parts []string
	for s.i < len(s.text) {
		from := s.i
		switch s.text[s.i] {
		case '"', '\'':
			s.str()
			parts = append(parts, s.text[from+1:max(s.i-1, from+1)])
		default:
			for s.i < len(s.text) && isBare(s.text[s.i]) {
				s.i++
			}
			if s.i == from {
				s.fail("no key")
				return ""
			}
			parts = append(parts, s.text[from:s.i])
		}
		j := skipSpace(s.text, s.i)
		if j >= len(s.text) || s.text[j] != '.' {
			break
		}
		s.i = skipSpace(s.text, j+1)
	}
	return strings.Join(parts, ".")
}

func isBare(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// value reads a value: a string, an array, an inline table, or one written
// bare, as a number.
func (s *scanner) value() {
	if s.i >= len(s.text) {
		s.fail("no value")
		return
	}
	switch s.text[s.i] {
	case '"', '\'':
		s.str()
	case '[':
		s.i++
		s.list(']', func() { s.value() })
	case '{':
		s.i++
		s.list('}', func() {
			s.key()
			s.i = skipSpace(s.text, s.i)
			s.expect("=")
			s.i = skipBlank(s.text, s.i)
			s.value()
		})
	default:
		from := s.i
		for s.i < len(s.text) && !strings.ContainsRune(",]}#\r\n", rune(s.text[s.i])) {
			s.i++
		}
		if strings.TrimSpace(s.text[from:s.i]) == "" {
			s.fail("no value")
		}
	}
}

// list reads the items of an array or inline table up to its closing
// bracket, each with item, separated by commas.
func (s *scanner) list(closing byte, item func()) {
	for s.err == nil {
		s.i = skipBlank(s.text, s.i)
		if s.i < len(s.text) && s.text[s.i] == closing {
			s.i++
			return
		}
		item()
		s.i = skipBlank(s.text, s.i)
		if s.i < len(s.text) && s.text[s.i] == ',' {
			s.i++
		} else if s.i >= len(s.text) || s.text[s.i] != closing {
			s.fail("no " + string(closing))
		}
	}
}

// elements returns the spans of the elements of the array at from.
func (s *scanner) elements(from int) []span {
	e := &scanner{text: s.text, i: from + 1}
	var spans []span
	e.list(']', func() {
		start := e.i
		e.value()
		spans = append(spans, span{start, e.i})
	})
	return spans
}

// str reads a string: basic or literal, on one line or several.
func (s *scanner) str() {
	q := s.text[s.i]
	if triple := strings.Repeat(string(q), 3); strings.HasPrefix(s.text[s.i:], triple) {
		s.i += 3
		for s.i < len(s.text) {
			switch {
			case q == '"' && s.text[s.i] == '\\':
				s.i += 2
			case strings.HasPrefix(s.text[s.i:], triple):
				s.i += 3
				for n := 0; n < 2 && s.i < len(s.text) && s.text[s.i] == q; n++ { // quotes just before the end
					s.i++
				}
				return
			default:
				s.i++
			}
		}
		s.fail("no end of string")
		return
	}
	for s.i++; s.i < len(s.text); s.i++ {
		switch c := s.text[s.i]; {
		case q == '"' && c == '\\':
			s.i++
		case c == q:
			s.i++
			return
		case c == '\n':
			s.fail("no end of string")
			return
		}
	}
	s.fail("no end of string")
}
