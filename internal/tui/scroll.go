package tui

import "fmt"

func visibleWindow(total, selected, maxRows int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if maxRows <= 0 || maxRows >= total {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}

	start := selected - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > total {
		start = total - maxRows
	}
	return start, start + maxRows
}

func (m *model) wizardListRows() int {
	rows := m.height - 14
	if rows < 5 {
		return 5
	}
	if rows > 18 {
		return 18
	}
	return rows
}

func (m *model) screenListRows(rowHeight int) int {
	if rowHeight <= 0 {
		rowHeight = 1
	}
	rows := (m.height - 12) / rowHeight
	if rows < 4 {
		return 4
	}
	if rows > 24 {
		return 24
	}
	return rows
}

func scrollStatus(start, end, total int) string {
	if total <= 0 || start == 0 && end >= total {
		return ""
	}
	return fmt.Sprintf("Showing %d-%d of %d", start+1, end, total)
}
