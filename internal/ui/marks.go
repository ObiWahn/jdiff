package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// markJumpMsg asks the file selector to show mark letter in another file of
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
		if rel, err := filepath.Rel(repo, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.ToSlash(rel)
		}
	}
	return abs
}

// setMark sets mark letter on the new-file line of displayed row v of d and
// returns the message to show.
func setMark(cfg *config.Config, d *fileDiff, letter string, v int) string {
	line := d.lineAt(right, v)
	if line < 0 {
		return "no line of the new file here"
	}
	m := config.Mark{Path: d.markPath, Line: line + 1, Text: strings.TrimSuffix(d.lines[right][line], "\r")}
	if err := cfg.SetMark(letter, m); err != nil {
		return "marks: " + err.Error()
	}
	return fmt.Sprintf("mark %s: %s:%d", letter, m.Path, m.Line)
}

// markLine returns the new-file line of mark m in d: its line when it still
// holds its text, else the nearest line that does, else its line number
// within the file, with a note. ok is false for an empty new file.
func (d *fileDiff) markLine(m config.Mark) (line int, note string, ok bool) {
	lines := d.lines[right]
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

// reveal returns the cursor on line of side s of d in the screen lines of
// sh, unfolding the block of unchanged rows the line is in.
func (d *fileDiff) reveal(s side, line int, sh shape) spot {
	row := d.rowOf[s][line]
	for _, b := range d.blocks {
		if row >= b.start && row < b.end && !d.blockOpen(b) {
			d.foldOpen[d.rows[b.start].Left] = true
			d.buildView()
			break
		}
	}
	return d.spotOf(s, line, sh)
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
	cfg           *config.Config
	open          bool
	cursor, top   int    // the selected mark, the first one shown
	msg           string // a problem saving the marks
	width, height int
}

// WithMarks returns m with the marks popup.
func WithMarks(cfg *config.Config, m tea.Model) *MarksPopup { return &MarksPopup{Model: m, cfg: cfg} }

func (p *MarksPopup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.move(0)
	case tea.KeyMsg:
		if cmd, ok := popupKeys(msg, p.open, "\\", p.key); ok {
			return p, cmd
		}
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// key handles k, pressed as msg, in the popup, or passes msg to the page.
func (p *MarksPopup) key(k string, msg tea.KeyMsg) tea.Cmd {
	if !p.open {
		if k == "\\" {
			p.open, p.cursor, p.top, p.msg = true, 0, 0, ""
			return nil
		}
		_, cmd := p.Model.Update(msg)
		return cmd
	}
	marks := p.cfg.Marks()
	switch k {
	case "\\", "esc", "q":
		p.open = false
	case "ctrl+c":
		return tea.Quit
	case "down", "j":
		p.move(1)
	case "up", "k":
		p.move(-1)
	case "d", "delete":
		if p.cursor < len(marks) {
			if err := p.cfg.DeleteMark(marks[p.cursor].Letter); err != nil {
				p.msg = "marks: " + err.Error()
			}
			p.move(0)
		}
	case "enter":
		if p.cursor < len(marks) {
			// Jump as ' and the letter do on the page.
			p.open = false
			p.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
			_, cmd := p.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(marks[p.cursor].Letter)})
			return cmd
		}
	}
	return nil
}

// boxHeight is the height of the popup: all marks when they fit, else the
// screen but a line above and below.
func (p *MarksPopup) boxHeight() int {
	return min(max(len(p.cfg.Marks()), 1)+2, max(p.height-2, minBoxHeight))
}

func (p *MarksPopup) move(delta int) {
	n := len(p.cfg.Marks())
	p.cursor = max(min(p.cursor+delta, n-1), 0)
	p.top = windowAround(p.cursor, p.top, p.boxHeight()-2, n).first
}

func (p *MarksPopup) View() string {
	base := p.Model.View()
	if !p.open || p.width == 0 {
		return base
	}
	t := p.cfg.Theme
	marks := p.cfg.Marks()
	all := make([]string, len(marks))
	widest := 0
	for i, mk := range marks {
		all[i] = fmt.Sprintf(" %s  %s:%d  %s ", mk.Letter, printable(mk.Path), mk.Line, printable(strings.TrimSpace(mk.Text)))
		widest = max(widest, ansi.StringWidth(all[i]))
	}
	w := windowAround(p.cursor, p.top, p.boxHeight()-2, len(marks))
	lines := w.rows(t, len(marks), func(i int) string {
		if i == p.cursor {
			return t.Cursor.Render(all[i])
		}
		return all[i]
	})
	if len(marks) == 0 {
		lines = []string{t.Hint.Render("no marks: m and a letter sets one")}
		widest = 40
	}
	title := t.CommitHash.Render(" marks ") + t.Hint.Render(" enter jump · d delete · esc close ")
	if p.msg != "" {
		title = t.CommitHash.Render(" marks ") + t.Hint.Render(" "+p.msg+" ")
	}
	width := min(max(widest+4, ansi.StringWidth(title)+4), p.width)
	return overlay(base, box(t, width, p.boxHeight(), title, lines), width, p.width)
}
