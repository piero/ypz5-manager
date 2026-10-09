package tui

import (
	"fmt"
	"os"
	"strings"
)

// style is an ANSI SGR style. Bubble Tea adapts the colors to what the
// terminal supports; NO_COLOR (https://no-color.org) disables them.
type style string

var colorEnabled = os.Getenv("NO_COLOR") == ""

func (s style) render(text string) string {
	if !colorEnabled || s == "" || text == "" {
		return text
	}

	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
}

func (s style) renderf(format string, args ...any) string {
	return s.render(fmt.Sprintf(format, args...))
}

// palette colors one side of the tool. The YP-Z5 (device or image) and
// the Mac's local files use different palettes, so it is always clear
// which filesystem a screen operates on.
type palette struct {
	name   string
	bar    style // title bar
	accent style // labels and key names
	dir    style
	file   style
	cursor style // the row under the cursor
	marked style // selection marks
}

var (
	// Blue and cyan: the YP-Z5 filesystem.
	devicePalette = palette{
		name:   " YP-Z5 ",
		bar:    "1;38;5;231;48;5;25",
		accent: "1;38;5;39",
		dir:    "1;38;5;45",
		cursor: "1;38;5;231;48;5;24",
		marked: "1;38;5;45",
	}

	// Amber and orange: the Mac's local files.
	localPalette = palette{
		name:   " MAC ",
		bar:    "1;38;5;16;48;5;214",
		accent: "1;38;5;214",
		dir:    "1;38;5;221",
		cursor: "1;38;5;16;48;5;179",
		marked: "1;38;5;208",
	}
)

// Status styles shared by both palettes.
const (
	dimStyle     style = "38;5;245"
	errorStyle   style = "1;38;5;203"
	warningStyle style = "1;38;5;221"
	successStyle style = "1;38;5;114"
	dangerBar    style = "1;38;5;231;48;5;160"
	readOnlyTag  style = "1;38;5;16;48;5;114"
	readWriteTag style = "1;38;5;231;48;5;160"
)

// titleBar renders the palette's name tag followed by a title.
func (p palette) titleBar(title string) string {
	return p.bar.render(p.name) + " " + p.accent.render(title)
}

// label renders "Label: value" with the label in the accent color.
// Trailing spaces in name align values: they go after the colon.
func (p palette) label(name, value string) string {
	trimmed := strings.TrimRight(name, " ")
	padding := strings.Repeat(" ", len(name)-len(trimmed))

	return p.accent.render(trimmed+":") + padding + " " + value
}

// help renders key/description pairs, e.g. help("q", "quit").
func (p palette) help(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)

	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, p.accent.render(pairs[i])+" "+dimStyle.render(pairs[i+1]))
	}

	return strings.Join(parts, "   ")
}

// row renders one list row. The cursor row is styled as a whole, so its
// text stays contiguous; other rows color the kind and name.
func (p palette) row(isCursor bool, prefix, kind, name, suffix string) string {
	if isCursor {
		return p.cursor.render("> " + prefix + kind + " " + name + suffix)
	}

	nameStyle := p.file
	if strings.TrimSpace(kind) == "DIR" {
		nameStyle = p.dir
	}

	return "  " + prefix + nameStyle.render(kind) + " " + nameStyle.render(name) + dimStyle.render(suffix)
}

func modeTag(writable bool) string {
	if writable {
		return readWriteTag.render(" READ/WRITE ")
	}

	return readOnlyTag.render(" READ-ONLY ")
}

func errorLine(err error) string {
	return errorStyle.render("Error:") + " " + err.Error()
}
