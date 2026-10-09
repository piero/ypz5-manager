package tui

import (
	"fmt"
	"strings"
)

// nameWidth is the column width for file names in lists. Longer names
// are truncated so that every entry stays on one line, which keeps the
// visible row count right.
const nameWidth = 40

// scrollOffset returns the first visible row of a list of n entries
// shown rows at a time. It moves offset as little as possible so that
// cursor stays visible. rows <= 0 means the height is unknown and the
// whole list is shown.
func scrollOffset(cursor, offset, n, rows int) int {
	if rows <= 0 || n <= rows {
		return 0
	}

	if cursor < offset {
		offset = cursor
	}

	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}

	return min(max(offset, 0), n-rows)
}

// visibleRange returns the [start, end) slice of a list to draw.
func visibleRange(offset, n, rows int) (int, int) {
	if rows <= 0 || n <= rows {
		return 0, n
	}

	return offset, min(offset+rows, n)
}

// positionLine describes which part of a long list is visible, or is
// empty when the whole list fits.
func positionLine(start, end, n int) string {
	if start == 0 && end == n {
		return ""
	}

	return fmt.Sprintf("  [%d-%d of %d]", start+1, end, n)
}

// listRows returns how many list entries fit on a screen of the given
// height around header and footer, or 0 when the height is unknown.
func listRows(height int, header, footer string) int {
	if height <= 0 {
		return 0
	}

	// Lines used by the header and footer, the position line, and a
	// spare line for the cursor after the last newline.
	used := strings.Count(header, "\n") + strings.Count(footer, "\n") + 2

	return max(height-used, 1)
}

// moveCursor moves cursor by delta within a list of n entries.
func moveCursor(cursor, delta, n int) int {
	if n == 0 {
		return 0
	}

	return min(max(cursor+delta, 0), n-1)
}

// truncateName shortens name to at most width characters, marking the
// cut with an ellipsis.
func truncateName(name string, width int) string {
	runes := []rune(name)

	if len(runes) <= width {
		return name
	}

	return string(runes[:width-1]) + "…"
}

// pageSize is how far PgUp/PgDn move in a list showing rows entries.
func pageSize(rows int) int {
	if rows <= 1 {
		return 10
	}

	return rows - 1
}
