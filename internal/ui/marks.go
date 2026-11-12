package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

// markJumpMsg asks the file list to show mark letter in another file of
// the diff, at screen row screenRow.
type markJumpMsg struct {
	letter    string
	screenRow int
}

// isMarkLetter reports whether k names a mark: a letter a-z or A-Z.
func isMarkLetter(k string) bool {
	if utf8.RuneCountInString(k) != 1 {
		return false
	}
	c := k[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// repoPath returns the path of file in marks: relative to the repository
// repo when inside it, else absolute.
func repoPath(repo, file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return file
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if repo != "" {
		if rel, err := filepath.Rel(repo, abs); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return abs
}

// setMark sets mark letter on the new-file line of displayed row v of a and
// returns the message to show.
func setMark(cfg *config.Config, a *fileAlign, letter string, v int) string {
	line := a.lineAt(right, v)
	if line < 0 {
		return "no line of the new file here"
	}
	m := config.Mark{Path: a.markPath, Line: line + 1, Text: strings.TrimSuffix(a.lines[right][line], "\r")}
	if err := cfg.SetMark(letter, m); err != nil {
		return "marks: " + err.Error()
	}
	return fmt.Sprintf("mark %s: %s:%d", letter, m.Path, m.Line)
}

// markLine returns the new-file line of mark m in a: its line when it still
// holds its text, else the nearest line that does, else its line number
// within the file, with a note. ok is false for an empty new file.
func (a *fileAlign) markLine(m config.Mark) (line int, note string, ok bool) {
	lines := a.lines[right]
	if len(lines) == 0 {
		return 0, "", false
	}
	want := m.Line - 1
	// Out from the old line until both directions left the file, which may
	// have shrunk far below it.
	for dist := 0; want-dist >= 0 || want+dist < len(lines); dist++ {
		for _, i := range []int{want - dist, want + dist} {
			// Without the \r of a \r\n line end, which may have changed.
			if i >= 0 && i < len(lines) && strings.TrimSuffix(lines[i], "\r") == strings.TrimSuffix(m.Text, "\r") {
				return i, "", true
			}
		}
	}
	return max(min(want, len(lines)-1), 0), "line changed", true
}

// reveal returns the cursor on line of side s of a in the screen lines of
// sh, unfolding the block of unchanged rows the line is in.
func (a *fileAlign) reveal(s side, line int, sh shape) spot {
	row := a.rowOf[s][line]
	for _, b := range a.blocks {
		if row >= b.start && row < b.end && !a.blockOpen(b) {
			a.foldOpen[a.rows[b.start].Left] = true
			a.buildView()
			break
		}
	}
	return a.spotOf(s, line, sh)
}

// markMsg returns the message for having jumped to mark letter.
func markMsg(letter, note string) string {
	if note != "" {
		return "mark " + letter + ": " + note
	}
	return "mark " + letter
}

// notInDiff returns the message for mark letter m outside the diff shown.
func notInDiff(letter string, m config.Mark) string {
	return fmt.Sprintf("mark %s: %s is not in this diff", letter, m.Path)
}

// MarksPopup wraps the top-level model and shows the jump marks of the
// repository in a popup over it on \: enter jumps to the selected mark, d or
// del deletes it.
type MarksPopup struct {
	tea.Model
	cfg  *config.Config
	open bool
	listPopup
	msg string // a problem saving the marks
}

// WithMarks returns m with the marks popup.
func WithMarks(cfg *config.Config, m tea.Model) *MarksPopup { return &MarksPopup{Model: m, cfg: cfg} }

func (p *MarksPopup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.move(0)
	case tea.KeyPressMsg:
		if cmd, ok := popupKeys(p.cfg.Keys, msg, p.open, "marks", p.key); ok {
			return p, cmd
		}
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// key handles k in the popup, or opens it on \\.
func (p *MarksPopup) key(k string) tea.Cmd {
	if !p.open {
		p.open, p.cursor, p.top, p.msg = true, 0, 0, ""
		return nil
	}
	if cmd, ok := popupNav(p.cfg.Keys, k, "marks", &p.open, p.move); ok {
		return cmd
	}
	marks := p.cfg.Marks()
	switch k {
	case "d", "delete":
		if p.cursor < len(marks) {
			if err := p.cfg.DeleteMark(marks[p.cursor].Letter); err != nil {
				p.msg = "marks: " + err.Error()
			}
			p.move(0)
		}
	case "enter":
		if p.cursor < len(marks) {
			// Jump as the key of jump, ', and the letter do on the page.
			p.open = false
			p.Model.Update(runeKey(keys.Name(p.cfg.Keys.Row("jump").Keys[0])))
			_, cmd := p.Model.Update(runeKey(marks[p.cursor].Letter))
			return cmd
		}
	}
	return nil
}

// boxHeight is the height of the popup.
func (p *MarksPopup) boxHeight() int { return popupHeight(len(p.cfg.Marks()), p.height) }

func (p *MarksPopup) move(delta int) { p.moveIn(delta, len(p.cfg.Marks())) }

func (p *MarksPopup) View() tea.View { return tea.NewView(p.frame()) }

func (p *MarksPopup) frame() string {
	base := p.Model.View().Content
	if !p.open || p.width == 0 {
		return base
	}
	t := p.cfg.Theme
	marks := p.cfg.Marks()
	all := make([]string, len(marks))
	for i, mk := range marks {
		all[i] = fmt.Sprintf(" %s  %s:%d  %s ", mk.Letter, printable(mk.Path), mk.Line, printable(strings.TrimSpace(mk.Text)))
	}
	return p.draw(t, base, popupFrame{"marks", "enter jump · d delete · esc close", p.msg}, all, "no marks: m and a letter sets one")
}
