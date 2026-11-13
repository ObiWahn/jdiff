package popup

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func bind(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }

// The name sits at the left of the top border, the top keys at its right,
// at least a line between them; the bottom keys at the right of the bottom
// border; the box is as wide as the widest of them.
func TestRender(t *testing.T) {
	f := Frame{Title: "Tags", Top: []key.Binding{bind("enter", "toggle"), bind("q", "close")}, Bottom: []key.Binding{bind("/", "search")}}
	got := ansi.Strip(f.Render("▶ like\n  skip", 80))
	want := strings.Join([]string{
		"╭─ Tags ─ enter toggle · q close ─╮",
		"│ ▶ like                          │",
		"│   skip                          │",
		"╰────────────────────── / search ─╯",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A key without words, as where the popup stands, has no space after it in
// the border.
func TestRenderKeyWithoutWords(t *testing.T) {
	f := Frame{Title: "Keys", Top: []key.Binding{bind("3/10", "")}}
	got := ansi.Strip(f.Render("abc", 80))
	want := "╭─ Keys ─ 3/10 ─╮\n│ abc           │\n╰───────────────╯"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := Hints([]key.Binding{bind("3/10", ""), bind("q", "close")}); got != "3/10 · q close" {
		t.Errorf("hints %q", got)
	}
}

// Without keys or a title the borders are plain lines; a disabled binding is
// left out.
func TestRenderPlain(t *testing.T) {
	off := bind("x", "gone")
	off.SetEnabled(false)
	got := ansi.Strip(Frame{Top: []key.Binding{off}}.Render("abc", 80))
	want := "╭─────╮\n│ abc │\n╰─────╯"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A box wider than its top border puts the free line between the name and
// the keys; without a name the keys still stand at the right, without keys
// the name at the left.
func TestRenderKeysRight(t *testing.T) {
	q := []key.Binding{bind("q", "close")}
	for _, c := range []struct {
		f    Frame
		want string
	}{
		{Frame{Title: "Tags", Top: q}, "╭─ Tags ────── q close ─╮"},
		{Frame{Top: q}, "╭───────────── q close ─╮"},
		{Frame{Title: "Tags"}, "╭─ Tags ────────────────╮"},
	} {
		if got := strings.Split(ansi.Strip(c.f.Render("a line of the content", 80)), "\n")[0]; got != c.want {
			t.Errorf("%+v: got %q, want %q", c.f, got, c.want)
		}
	}
}

// Too narrow: the top keys are cut before the name, the bottom keys and the
// lines are cut with …; every row has the same width.
func TestRenderCuts(t *testing.T) {
	f := Frame{Title: "Columns", Top: []key.Binding{bind("enter", "show"), bind("t", "truncate")}, Bottom: []key.Binding{bind("backspace", "erase a digit")}}
	rows := strings.Split(ansi.Strip(f.Render("a very long line of content", 24)), "\n")
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 24 {
			t.Errorf("row %q is %d wide, want 24", r, w)
		}
	}
	if !strings.HasPrefix(rows[0], "╭─ Columns ─ ") || !strings.Contains(rows[0], "…") {
		t.Errorf("top %q: want the name whole, the keys cut", rows[0])
	}
	if !strings.Contains(rows[1], "…") || !strings.Contains(rows[2], "…") {
		t.Errorf("line %q or bottom %q not cut", rows[1], rows[2])
	}
}

// A message takes the place of the bottom keys, and widens the box up to the
// width given; a long one is cut with … and leaves the top border alone.
func TestRenderMsg(t *testing.T) {
	f := Frame{Title: "Keys", Top: []key.Binding{bind("q", "close")}, Bottom: []key.Binding{bind("/", "search")}, Msg: "ctrl+s is already save"}
	got := ansi.Strip(f.Render("abc", 80))
	want := strings.Join([]string{
		"╭─ Keys ───────── q close ─╮",
		"│ abc                      │",
		"╰─ ctrl+s is already save ─╯",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	rows := strings.Split(ansi.Strip(f.Render("abc", 20)), "\n")
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 20 {
			t.Errorf("row %q is %d wide, want 20", r, w)
		}
	}
	if rows[0] != "╭─ Keys ─ q close ─╮" || !strings.Contains(rows[2], "…") || strings.Contains(rows[2], "search") {
		t.Errorf("top %q, bottom %q: want the top whole, the message cut", rows[0], rows[2])
	}

	f.Bottom = nil
	if got := strings.Split(ansi.Strip(f.Render("abc", 80)), "\n")[2]; got != "╰─ ctrl+s is already save ─╯" {
		t.Errorf("without bottom keys: %q", got)
	}
}

// Only the border and the border texts are coloured; the content keeps its
// own colours.
func TestStyles(t *testing.T) {
	s := DefaultStyles()
	f := Frame{Title: "T", Top: []key.Binding{bind("q", "close")}, Styles: s}
	got := f.Render("plain", 80)
	for _, want := range []string{s.Title.Render("T"), s.Hint.Render("q close"), s.Border.Render("│") + " plain"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%q", want, got)
		}
	}
	if s.Border.GetForeground() != lipgloss.Blue || s.Title.GetForeground() != lipgloss.Yellow {
		t.Errorf("default styles %v, %v: want a blue border, a yellow name", s.Border.GetForeground(), s.Title.GetForeground())
	}
	if s.Msg.GetForeground() != lipgloss.Red || !s.Msg.GetBold() {
		t.Errorf("default message style %v, bold %v: want bold red", s.Msg.GetForeground(), s.Msg.GetBold())
	}

	f.Bottom, f.Msg = []key.Binding{bind("/", "search")}, "saved"
	got = f.Render("plain", 80)
	if !strings.Contains(got, s.Msg.Render("saved")) || strings.Contains(got, "search") {
		t.Errorf("want the message in its style, no bottom keys, in\n%q", got)
	}
}
