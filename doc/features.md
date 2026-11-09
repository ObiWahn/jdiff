# Features

## Compared with delta

jdiff is inspired by [delta](https://github.com/dandavison/delta), the syntax-highlighting pager for git written in Rust.
delta formats the output of git once and hands it to a pager; jdiff is a program you work in, so it can do what a pager cannot.
delta highlights syntax; jdiff does not.

- **Resizing:** the diff is laid out again when the terminal changes size; delta lays it out once, for the width it started with.
- **Switching the layout:** two panes or one column like `git diff`; `s` switches between them while you look at the diff. In delta, side by side is chosen at startup.
- **Wrap:** `w` switches between wrapping long lines onto further lines, marked `↪`, and cutting them; delta wraps a line as often as `--wrap-max-lines` allows, set at startup, and truncates the rest.
  - A cut line that goes on past its pane ends in `»`, one scrolled out to the left (`h` `l`) starts with `«`.
- **More or less context:** long unchanged runs collapse to a single row; `space` opens or closes the nearest one, `ctrl+space` all of them. delta shows the context lines git gave it.
- **Walking the history:** `jdiff log` lists the commits, opens a commit's files one by one, and `N` `P` step to the next or previous commit, keeping the open file.
- **Following moved code:** like delta, jdiff colors blocks moved within a file or between the files of one commit or diff, magenta where they moved away and cyan where they moved to, as git's `--color-moved` does. `g` jumps to the other end, also into another file.
- **Auto reload:** with [`R`](keybindings.md#auto-reload), files on disk are [watched](usage.md#auto-reload-and-file-watching): when you save an edit, its diff updates while jdiff is open, and anchors, folds and the cursor follow their lines; in `jdiff diff` and `jdiff log` all is loaded again whenever the repository changes, so new files and commits show up too. `r`, `F5` or `ctrl+r` runs `git diff` or `git log` again, or lists the directories again, by hand. delta shows the diff as it was when git ran.
- **Anchors:** mark a line with `enter`, then its partner in the other pane. jdiff realigns around it. Anchors that would cross are offered for replacement.
- **Jump marks:** `m` plus a letter marks a line of the new file, `'` plus the letter jumps back to it within the diff. `\` lists the marks; there, `enter` jumps and `d` deletes.
  - Marks are kept per repository and survive restarts.
  - A mark finds its line again by its text when the file changed.

## What else it can do

- **Compare two files**, two directories (`git difftool --dir-diff`), what `git diff` shows (`jdiff diff`, e.g. `--cached` for the index), or the commits of `git log`.
- **Switch what is compared:** `d` asks in a popup for the arguments of `git diff`, e.g. `HEAD~3 HEAD`, quoted as in a shell for paths with spaces, and shows its files instead of what is shown, from any view, also from the log.
- **Word-level highlighting** of changed lines. Indentation changes are shown character by character, so only the added or removed part is marked. Lines longer than 4096 bytes, as of minified code, are shown without it.
- **Flow view (`f`):** all files of a diff, or all commits of the log, one after the other in one scrollable view. Commits load as they come near the cursor.
  - `c` shows the full commit messages or only their subjects.
  - `C` keeps the cursor inside the current commit.
- **Search:** `/` searches the diff, as plain text or a regular expression, `.` and `,` go to the next and previous match, in the flow view through all files, in the log within the current commit.
- **Long lists:** when the file or commit list, the help or the jump list goes on above or below, its first or last row says so, e.g. `↓ 21 more`; an arrow never stands for a single item, which is shown instead.
- **Previews:** the commit list shows the selected commit's message, the file list a preview of the selected file's diff (`v`). With the preview, the file list takes at most 15 rows, so that the preview gets the rest of a tall screen.
- **Whitespace:** changed indentation, tabs and runs of spaces are marked as `·` and `→`, only their added or removed part, not single spaces between changed words.
  - `W` draws all whitespace so and line ends as `¶` (`line_end`).
  - `ctrl+w` turns the marking of changed whitespace off.
  - `t` shows trailing whitespace.
  - Changed `\r\n` line ends show as `␍`, all of them with `e`.
  - Invisible characters, among them those that change the text direction, zero-width spaces and joiners, show as `☠`. Spaces that take room, as the no-break space, are drawn as they are; emoji joined into one sign, as a family, show as their emoji.
- **16-color output:** every color is an ANSI color, so your terminal palette decides how jdiff looks.
