// Package ui implements the terminal UI of jdiff. App is the top-level model:
// it shows a page, Diff (the files of a comparison, each opened as a
// FileDiff), Log (the commit list, each commit with its Diff) or Readme, in
// the search bar and the popups, and keeps what all pages share. Flow shows
// the diffs of all files one after the other, from a Diff or the Log.
package ui
