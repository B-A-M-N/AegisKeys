package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// View renders the full screen: sidebar + content + footer. Returns a
// tea.View with AltScreen set so the terminal does not accumulate scrollback.
func (m *model) View() tea.View {
	s := m.styles

	if !m.unlocked && m.vaultExists {
		v := tea.NewView(m.lockedView(s))
		v.AltScreen = true
		return v
	}

	if m.width < 100 || m.height < 30 {
		v := tea.NewView(m.renderTooSmall(s))
		v.AltScreen = true
		return v
	}

	w, h := m.width, m.height
	grid := make([][]gridCell, h)
	priority := make([][]int, h)
	for y := range grid {
		grid[y] = make([]gridCell, w)
		priority[y] = make([]int, w)
	}

	protected := []Rect{
		{X: 0, Y: 0, W: sidebarWidth, H: h - 1}, // sidebar
		{X: 0, Y: h - 1, W: w, H: 1},            // footer
	}

	cx := sidebarWidth
	cw := w - sidebarWidth

	// Content is not blanket-protected: foreground text overwrites the matrix by
	// priority, while unused content whitespace can keep breathing behind it.
	contentStyled := m.contentForeground(s)

	// Modal protection.
	modalActive := m.modal != modalNone
	var modalRect Rect
	if modalActive {
		modalW := minInt(cw-8, 64)
		modalH := 16
		mx := cx + (cw-modalW)/2
		my := (h - modalH) / 2
		modalRect = Rect{X: mx, Y: my, W: modalW, H: modalH}
		protected = append(protected, modalRect)
	}

	// Layer 1: Matrix background (only in unprotected cells).
	if m.matrix != nil {
		buf := NewMatrixBuffer(w, h)
		buf.Protected = protected
		m.matrix.Render(buf)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				ch := buf.CellAt(x, y)
				if ch.colorIdx > 0 {
					if 1 > priority[y][x] && !grid[y][x].protected {
						grid[y][x] = gridCell{ch: ch.ch, styleID: ch.colorIdx}
						priority[y][x] = 1
					}
				}
			}
		}
	}

	// Layer 2: Sidebar (writes spaces to protect from matrix).
	// Use styled rendering to preserve Lip Gloss colors.
	sidebarStyled := m.renderSidebar(s)
	for y, line := range strings.Split(sidebarStyled, "\n") {
		if y >= h-1 {
			break
		}
		segments := parseANSILine(line)
		if len(segments) == 1 && segments[0].fg == "" && segments[0].bg == "" && !segments[0].bold {
			// No inline styling: use coarse styleID.
			writeLine(grid, priority, 0, y, line, sidebarStyleID(stripANSI(line)), 2, w, h, true)
		} else {
			writeLineStyled(grid, priority, 0, y, segments, 2, w, h, true)
		}
	}

	// Layer 3: Content (writes spaces to protect from matrix).
	// Use styled content rendering to preserve Lip Gloss colors.
	for y, line := range strings.Split(contentStyled, "\n") {
		if y >= h-1 {
			break
		}
		segments := parseANSILine(line)
		if len(segments) == 1 && segments[0].fg == "" && segments[0].bg == "" && !segments[0].bold {
			// No inline styling: fall back to coarse styleID for backward compat.
			writeLine(grid, priority, cx, y, line, contentStyleID(stripANSI(line), y), 2, w, h, true)
		} else {
			writeLineStyled(grid, priority, cx, y, segments, 2, w, h, true)
		}
	}

	// Layer 4: Footer.
	footerStyled := m.footerText()
	footerSegments := parseANSILine(footerStyled)
	if len(footerSegments) == 1 && footerSegments[0].fg == "" && footerSegments[0].bg == "" && !footerSegments[0].bold {
		writeLine(grid, priority, 0, h-1, footerStyled, 31, 3, w, h, true)
	} else {
		writeLineStyled(grid, priority, 0, h-1, footerSegments, 3, w, h, true)
	}

	// Layer 5: Modal.
	if modalActive {
		m.drawModal(grid, priority, modalRect, s, w, h)
	}

	// Render grid to string.
	frame := 0
	if m.matrix != nil {
		frame = m.matrix.Frame
	}
	full := renderGridFrame(grid, s, w, h, frame)

	v := tea.NewView(full)
	v.AltScreen = true
	return v
}

// gridCell stores a plain character and semantic style information.
type gridCell struct {
	ch        rune
	styleID   int
	fg        string // hex foreground color, empty means "use styleID default"
	bg        string // hex background color, empty means "use styleID default"
	bold      bool
	protected bool // if true, matrix cannot overwrite this cell
}

// stripANSI removes ANSI escape sequences.
func stripANSI(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' {
			i++
			if i < len(runes) && runes[i] == '[' {
				i++
				for i < len(runes) && runes[i] != 'm' {
					i++
				}
			}
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// ansiSegment holds foreground color and bold state for a styled run.
type ansiSegment struct {
	text string
	fg   string
	bg   string
	bold bool
}

// parseANSILine extracts styled segments from a Lip Gloss-rendered line.
// This is a focused parser that handles the SGR sequences Charm emits.
func parseANSILine(s string) []ansiSegment {
	var segments []ansiSegment
	runes := []rune(s)
	var cur ansiSegment
	i := 0
	for i < len(runes) {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Flush current segment.
			if cur.text != "" {
				segments = append(segments, cur)
				cur = ansiSegment{fg: cur.fg, bg: cur.bg, bold: cur.bold}
			}
			// Parse SGR.
			i += 2 // skip \x1b[
			start := i
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			codes := string(runes[start:i])
			i++ // skip 'm'
			// Parse SGR params with index-based handling for multi-number
			// sequences like 38;2;r;g;b (truecolor) and 38;5;n (256-color). A naive
			// per-param switch fails because the sub-params are split apart; we must
			// look ahead within the params slice.
			params := splitSGRParams(codes)
			for j := 0; j < len(params); j++ {
				switch params[j] {
				case "0":
					cur.fg = ""
					cur.bg = ""
					cur.bold = false
				case "1":
					cur.bold = true
				case "38":
					// Foreground: 38;2;r;g;b (truecolor) or 38;5;n (256-color).
					if j+4 < len(params) && params[j+1] == "2" {
						r := sgrDec(params[j+2])
						g := sgrDec(params[j+3])
						b := sgrDec(params[j+4])
						cur.fg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
						j += 4
					} else if j+2 < len(params) && params[j+1] == "5" {
						cur.fg = ansi256ToHex(sgrDec(params[j+2]))
						j += 2
					}
				case "48":
					// Background: 48;2;r;g;b (truecolor) or 48;5;n (256-color).
					if j+4 < len(params) && params[j+1] == "2" {
						r := sgrDec(params[j+2])
						g := sgrDec(params[j+3])
						b := sgrDec(params[j+4])
						cur.bg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
						j += 4
					} else if j+2 < len(params) && params[j+1] == "5" {
						cur.bg = ansi256ToHex(sgrDec(params[j+2]))
						j += 2
					}
				case "30", "31", "32", "33", "34", "35", "36", "37":
					// Standard foreground.
					cur.fg = ansiColorHex(params[j], false)
				case "40", "41", "42", "43", "44", "45", "46", "47":
					// Standard background.
					cur.bg = ansiColorHex(params[j], true)
				case "90", "91", "92", "93", "94", "95", "96", "97":
					// Bright foreground.
					cur.fg = ansiColorHex(params[j], false)
				case "100", "101", "102", "103", "104", "105", "106", "107":
					// Bright background.
					cur.bg = ansiColorHex(params[j], true)
				}
			}
			continue
		}
		cur.text += string(runes[i])
		i++
	}
	if cur.text != "" {
		segments = append(segments, cur)
	}
	if len(segments) == 0 {
		segments = append(segments, ansiSegment{text: stripANSI(s)})
	}
	return segments
}

func splitSGRParams(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ";")
}

func sgrDec(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// ansiColorHex maps standard SGR color codes to hex.
func ansiColorHex(code string, bg bool) string {
	base := 0
	switch code {
	case "30", "40":
		base = 0
	case "31", "41":
		base = 1
	case "32", "42":
		base = 2
	case "33", "43":
		base = 3
	case "34", "44":
		base = 4
	case "35", "45":
		base = 5
	case "36", "46":
		base = 6
	case "37", "47":
		base = 7
	case "90", "100":
		base = 8
	case "91", "101":
		base = 9
	case "92", "102":
		base = 10
	case "93", "103":
		base = 11
	case "94", "104":
		base = 12
	case "95", "105":
		base = 13
	case "96", "106":
		base = 14
	case "97", "107":
		base = 15
	default:
		return ""
	}
	return ansi256ToHex(base)
}

// ansi256ToHex maps the first 16 ANSI colors (standard + bright) to hex.
// The 216-color cube and grayscale ramp are approximate.
func ansi256ToHex(n int) string {
	if n < 16 {
		standard := [16]string{
			"#000000", "#cc0000", "#00cc00", "#cccc00",
			"#0000cc", "#cc00cc", "#00cccc", "#cccccc",
			"#666666", "#ff0000", "#00ff00", "#ffff00",
			"#0000ff", "#ff00ff", "#00ffff", "#ffffff",
		}
		return standard[n]
	}
	return ""
}

// extractHexColorFromRune is a fallback heuristic: if a line contains a hex color
// in its markup, we capture it. This helps when Charm emits styles via lipgloss.
func extractHexColorFromRune(line string) string {
	// Lip Gloss renders hex colors as 38;2;r;g;b ANSI sequences; parseANSILine handles those.
	// This function is unused but retained for future expansion.
	_ = line
	return ""
}

// writeLine writes text into the grid at (x,y).
func writeLine(grid [][]gridCell, priority [][]int, x, y int, text string, styleID int, layer int, w, h int, writeSpaces bool) {
	if y < 0 || y >= h {
		return
	}
	text = stripANSI(text)
	curX := x
	for _, r := range text {
		if curX >= w {
			break
		}
		if r == '\n' || r == '\r' {
			break
		}
		if r == ' ' && !writeSpaces {
			curX++
			continue
		}
		if curX >= 0 && layer >= priority[y][curX] {
			grid[y][curX] = gridCell{ch: r, styleID: styleID}
			priority[y][curX] = layer
		}
		curX++
	}
}

// protectModalArea marks all cells within the modal rectangle as protected
// so the matrix cannot bleed into the modal regardless of how content is rendered.
func protectModalArea(grid [][]gridCell, priority [][]int, r Rect, layer int, w, h int) {
	for y := r.Y; y < r.Y+r.H && y < h; y++ {
		for x := r.X; x < r.X+r.W && x < w; x++ {
			if x >= 0 && y >= 0 && layer > priority[y][x] {
				grid[y][x] = gridCell{ch: ' ', styleID: 20, protected: true}
				priority[y][x] = layer
			}
		}
	}
}

// writeLineStyled writes text with per-character color from parsed ANSI segments.
// Unlike writeLine, this preserves fg/bg from the styled input rather than
// replacing it with a single styleID. Spaces are written when writeSpaces is
// true so the matrix does not bleed through.
func writeLineStyled(grid [][]gridCell, priority [][]int, x, y int, segments []ansiSegment, layer int, w, h int, writeSpaces bool) {
	if y < 0 || y >= h {
		return
	}
	curX := x
	for _, seg := range segments {
		for _, r := range seg.text {
			if curX >= w {
				break
			}
			if r == '\n' || r == '\r' {
				break
			}
			if r == ' ' && !writeSpaces {
				curX++
				continue
			}
			if curX >= 0 && layer >= priority[y][curX] {
				grid[y][curX] = gridCell{ch: r, fg: seg.fg, bg: seg.bg, bold: seg.bold}
				priority[y][curX] = layer
			}
			curX++
		}
	}
}

// drawModal draws a modal panel into the grid.
func (m *model) drawModal(grid [][]gridCell, priority [][]int, r Rect, s *Styles, w, h int) {
	bgLayer := 5
	borderLayer := 6
	contentLayer := 7

	// Background fill — mark as protected so matrix cannot bleed through.
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if x >= 0 && x < w && y >= 0 && y < h {
				if bgLayer > priority[y][x] {
					grid[y][x] = gridCell{ch: ' ', styleID: 20, protected: true}
					priority[y][x] = bgLayer
				}
			}
		}
	}
	// Border.
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		if x < w {
			if r.Y < h && borderLayer > priority[r.Y][x] {
				grid[r.Y][x] = gridCell{ch: '-', styleID: 21}
				priority[r.Y][x] = borderLayer
			}
			if r.Y+r.H-1 < h && borderLayer > priority[r.Y+r.H-1][x] {
				grid[r.Y+r.H-1][x] = gridCell{ch: '-', styleID: 21}
				priority[r.Y+r.H-1][x] = borderLayer
			}
		}
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		if y < h {
			if r.X < w && borderLayer > priority[y][r.X] {
				grid[y][r.X] = gridCell{ch: '|', styleID: 21}
				priority[y][r.X] = borderLayer
			}
			if r.X+r.W-1 < w && borderLayer > priority[y][r.X+r.W-1] {
				grid[y][r.X+r.W-1] = gridCell{ch: '|', styleID: 21}
				priority[y][r.X+r.W-1] = borderLayer
			}
		}
	}
	// Corners.
	if r.X < w && r.Y < h && borderLayer > priority[r.Y][r.X] {
		grid[r.Y][r.X] = gridCell{ch: '+', styleID: 21}
		priority[r.Y][r.X] = borderLayer
	}
	if r.X+r.W-1 < w && r.Y < h && borderLayer > priority[r.Y][r.X+r.W-1] {
		grid[r.Y][r.X+r.W-1] = gridCell{ch: '+', styleID: 21}
		priority[r.Y][r.X+r.W-1] = borderLayer
	}
	if r.X < w && r.Y+r.H-1 < h && borderLayer > priority[r.Y+r.H-1][r.X] {
		grid[r.Y+r.H-1][r.X] = gridCell{ch: '+', styleID: 21}
		priority[r.Y+r.H-1][r.X] = borderLayer
	}
	if r.X+r.W-1 < w && r.Y+r.H-1 < h && borderLayer > priority[r.Y+r.H-1][r.X+r.W-1] {
		grid[r.Y+r.H-1][r.X+r.W-1] = gridCell{ch: '+', styleID: 21}
		priority[r.Y+r.H-1][r.X+r.W-1] = borderLayer
	}

	// Title.
	title := "Detail"
	if m.modal == modalConfirmDelete {
		title = "Confirm Delete"
	}
	if m.modal == modalAdd {
		title = "Add"
	}
	if m.modal == modalEdit {
		title = "Edit"
	}
	writeLine(grid, priority, r.X+2, r.Y+1, title, 22, contentLayer, w, h, false)

	// Body.
	var body string
	switch m.modal {
	case modalDetail:
		body = m.renderDetailModal()
	case modalConfirmDelete:
		body = m.renderDeleteModal()
	}
	bodyLines := strings.Split(body, "\n")
	for i, line := range bodyLines {
		if i+3 >= r.H-1 {
			break
		}
		line = truncate(line, r.W-4)
		writeLine(grid, priority, r.X+2, r.Y+3+i, line, 23, contentLayer, w, h, false)
	}

	// Render the add/edit input field.
	if m.modal == modalAdd || m.modal == modalEdit {
		fields := m.addFields()
		if m.modal == modalEdit {
			fields = m.editFields()
		}
		stepText := m.modalPrompt
		if len(fields) > 0 {
			stepText = fmt.Sprintf("%s  (%d/%d)", m.modalPrompt, m.addStep+1, len(fields))
		}
		writeLine(grid, priority, r.X+2, r.Y+3, stepText, 22, contentLayer, w, h, false)
		inputView := stripANSI(m.addInput.View())
		writeLine(grid, priority, r.X+2, r.Y+5, inputView, 23, contentLayer+1, w, h, false)
		writeLine(grid, priority, r.X+2, r.Y+7, "Enter next/save  |  Esc cancel", 23, contentLayer, w, h, false)
	}

	// Render the key add form with all fields visible.
	if m.modal == modalAddKey {
		m.drawKeyAddForm(grid, priority, r, s, w, h)
	}

	// Re-protect the entire modal area after content is drawn,
	// so the matrix cannot bleed through regardless of how content was rendered.
	protectModalArea(grid, priority, r, bgLayer, w, h)
}

// drawKeyAddForm renders the multi-field key add form with the active field highlighted.
func (m *model) drawKeyAddForm(grid [][]gridCell, priority [][]int, r Rect, s *Styles, w, h int) {
	contentLayer := 7
	labels := []string{"Provider:", "Label:", "Secret:", "Tags:"}
	values := []string{
		m.keyForm.providerSlug,
		m.keyForm.label,
		"",
		m.keyForm.tags,
	}
	// Secret is always masked.
	if m.keyForm.secret != "" {
		values[2] = "••••••••"
	}

	for i, label := range labels {
		y := r.Y + 2 + i*3
		if y >= r.Y+r.H-1 {
			break
		}
		rowStyle := 23
		if i == m.keyFormActive {
			rowStyle = 12 // highlight active row
		}
		labelStr := fmt.Sprintf("%-10s", label)
		writeLine(grid, priority, r.X+2, y, labelStr, 22, contentLayer, w, h, false)
		writeLine(grid, priority, r.X+13, y, values[i], rowStyle, contentLayer+1, w, h, false)
	}

	// Show provider selection list when provider field is active.
	if m.keyFormActive == 0 && len(m.providers.Providers) > 0 {
		listY := r.Y + 2 + 4*3
		if listY < r.Y+r.H-1 {
			writeLine(grid, priority, r.X+2, listY, "Available providers:", 22, contentLayer, w, h, false)
			for i, p := range m.providers.Providers {
				marker := " "
				style := 23
				if i == m.keyForm.providerIdx {
					marker = "›"
					style = 12
				}
				lineY := listY + 1 + i
				if lineY >= r.Y+r.H-1 {
					break
				}
				display := fmt.Sprintf("%-15s %s", p.Name, p.Compatibility)
				writeLine(grid, priority, r.X+4, lineY, marker+display, style, contentLayer, w, h, false)
			}
		}
	}

	// Help text at bottom.
	helpY := r.Y + r.H - 2
	if helpY < r.Y+r.H {
		writeLine(grid, priority, r.X+2, helpY, "Tab: next · ↑/↓: select provider · Enter: save · Esc: cancel", 23, contentLayer, w, h, false)
	}
}

// renderGridFrame converts the grid to a string, grouping runs by style.
func renderGridFrame(grid [][]gridCell, s *Styles, w, h int, frame int) string {
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		runStyle := -999
		var run strings.Builder
		runFg := ""
		runBg := ""
		runBold := false

		flush := func() {
			if run.Len() == 0 {
				return
			}
			st := cellStyleFrame(runStyle, s, frame)
			if runFg != "" {
				st = st.Foreground(lipgloss.Color(runFg))
			}
			if runBg != "" {
				st = st.Background(lipgloss.Color(runBg))
			}
			if runBold {
				st = st.Bold(true)
			}
			b.WriteString(st.Render(run.String()))
			run.Reset()
		}

		for x := 0; x < w; x++ {
			c := grid[y][x]
			ch := c.ch
			styleID := c.styleID

			if ch == 0 {
				ch = ' '
				styleID = 0
			}

			// Determine if this cell has custom styling that breaks the run.
			hasCustom := c.fg != "" || c.bg != "" || c.bold
			styleChanged := styleID != runStyle
			fgChanged := c.fg != runFg
			bgChanged := c.bg != runBg
			boldChanged := c.bold != runBold

			if hasCustom {
				// Flush any pending run before handling this cell.
				flush()
				runStyle = styleID
				runFg = c.fg
				runBg = c.bg
				runBold = c.bold
				run.WriteRune(ch)
				// Flush immediately after a custom-styled cell to avoid
				// merging with neighbors that may have different styling.
				flush()
				runStyle = -999
				runFg = ""
				runBg = ""
				runBold = false
				continue
			}

			if styleChanged || fgChanged || bgChanged || boldChanged {
				flush()
				runStyle = styleID
				runFg = c.fg
				runBg = c.bg
				runBold = c.bold
			}
			run.WriteRune(ch)
		}
		flush()

		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// cellStyleFrame returns the Lip Gloss style for a style ID with animation frame.
func cellStyleFrame(id int, s *Styles, frame int) lipgloss.Style {
	switch id {
	case 20:
		return s.CellModalBg
	case 21:
		return s.CellModalBorder
	case 22:
		return s.CellModalTitle
	case 23:
		return s.CellModalText
	case 28:
		return s.CellDanger
	case 27:
		return s.CellSuccess
	case 26:
		return s.CellHeader
	case 25:
		return s.CellAccent.Foreground(lipgloss.Color(animatedColor(frame, "#7b4dcc", "#9b6dff", "#b88cff", "#9b6dff")))
	case 24:
		return s.CellTitle.Foreground(lipgloss.Color(animatedColor(frame, "#ffffff", "#d7c5ff", "#b88cff", "#ffffff")))
	case 12:
		return s.CellSelected
	case 11:
		return s.CellMuted
	case 10:
		return s.CellBody
	case 30:
		return s.CellSidebar
	case 31:
		return s.CellFooter
	case 7:
		return s.MatrixRed
	case 6:
		return s.MatrixWhite
	case 5:
		return s.MatrixBright.Foreground(lipgloss.Color(animatedColor(frame, "#a866ff", "#c79cff", "#ffffff", "#c79cff")))
	case 4:
		return s.MatrixPurple.Foreground(lipgloss.Color(animatedColor(frame, "#6a3fb0", "#7b4dcc", "#9b6dff", "#7b4dcc")))
	case 3:
		return s.MatrixDeep
	case 2:
		return s.MatrixDim
	case 1:
		return s.MatrixVoid
	default:
		return s.CellDefault
	}
}

// matrixColor returns the hex color for a matrix cell based on its palette index,
// trail position, and frame for per-cell shimmer.
func matrixColor(colorIdx, trailPos, frame int) string {
	if colorIdx < 0 {
		colorIdx = 0
	}
	if colorIdx >= len(matrixPalette) {
		colorIdx = len(matrixPalette) - 1
	}
	// Per-cell shimmer: some cells brighten based on position and frame.
	if (frame+trailPos*7)%23 == 0 && colorIdx < len(matrixPalette)-1 {
		colorIdx++
	}
	return matrixPalette[colorIdx]
}

func sidebarStyleID(line string) int {
	trim := strings.TrimSpace(stripANSI(line))
	switch {
	case strings.HasPrefix(trim, ">"):
		return 12
	case strings.Contains(trim, "AegisKeys"):
		return 24
	case strings.HasPrefix(trim, "vault:"):
		return 25
	default:
		return 30
	}
}

func contentStyleID(line string, row int) int {
	trim := strings.TrimSpace(stripANSI(line))
	switch {
	case row == 0 && trim != "":
		return 24
	case strings.HasPrefix(trim, "›") || strings.HasPrefix(trim, ">"):
		return 12
	case strings.Contains(trim, "NAME") && strings.Contains(trim, "SLUG"):
		return 26
	case strings.HasPrefix(trim, "Recent activity") || strings.HasPrefix(trim, "Will inject") || strings.HasPrefix(trim, "Keys"):
		return 25
	case strings.HasPrefix(trim, "OK") || strings.HasPrefix(trim, "[OK") || strings.Contains(trim, "unlocked"):
		return 27
	case strings.HasPrefix(trim, "FAIL") || strings.HasPrefix(trim, "[FAIL") || strings.Contains(trim, "locked"):
		return 28
	case strings.HasPrefix(trim, "No ") || strings.HasPrefix(trim, "Press ") || strings.HasPrefix(trim, "Edit "):
		return 11
	default:
		return 10
	}
}

func animatedColor(frame int, colors ...string) string {
	if len(colors) == 0 {
		return "#9b6dff"
	}
	idx := (frame / 8) % len(colors)
	if idx < 0 {
		idx = 0
	}
	return colors[idx]
}

// renderTooSmall shows a warning when the terminal is too small.
func (m *model) renderTooSmall(s *Styles) string {
	msg := fmt.Sprintf("AegisKeys needs a larger terminal.\nMinimum: 100x30\nCurrent: %dx%d", m.width, m.height)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(s.Warning.Render(msg))
}

// renderSidebar builds the navigation sidebar.
func (m *model) renderSidebar(s *Styles) string {
	var b strings.Builder
	b.WriteString(s.SidebarHeader.Render(" AegisKeys"))
	b.WriteString("\n")
	b.WriteString(s.SidebarFooter.Render(" " + m.version))
	b.WriteString("\n\n")

	for i, sc := range screenDefs {
		label := sc.label
		marker := " "
		style := s.SidebarItem
		if i == int(m.active) {
			marker = ">"
			style = s.SidebarItemActive
		}
		if m.focus == focusSidebar && i == int(m.active) {
			style = s.SidebarItemActive
		}
		b.WriteString(style.Render(marker + " " + label))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	status := "locked"
	switch {
	case !m.vaultExists:
		status = "no vault"
	case m.unlocked:
		status = "unlocked"
	}
	b.WriteString(s.SidebarFooter.Render(" vault: " + status))
	b.WriteString("\n\n")
	b.WriteString(m.sidebarHints(s))
	return b.String()
}

// sidebarHints renders contextual command hints based on the active screen.
func (m *model) sidebarHints(s *Styles) string {
	rows := m.contextHints()

	var b strings.Builder
	b.WriteString(s.Muted.Render(" commands"))
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString(" ")
		b.WriteString(s.KeyMasked.Render(padRightHint(r.key, 8)))
		b.WriteString(s.Muted.Render(r.desc))
		b.WriteString("\n")
	}
	return b.String()
}

type hintRow struct {
	key  string
	desc string
}

func (m *model) contextHints() []hintRow {
	if m.focus == focusModal || m.modal != modalNone {
		return []hintRow{
			{"Enter", "next / confirm"},
			{"Esc", "cancel"},
			{"Tab", "next field"},
		}
	}

	switch m.active {
	case screenProviders:
		return []hintRow{
			{"Z", "new provider"},
			{"E", "edit provider"},
			{"X", "delete provider"},
			{"Enter", "details"},
		}
	case screenKeys:
		return []hintRow{
			{"Z", "add key"},
			{"E", "edit key"},
			{"R", "rotate key"},
			{"X", "delete key"},
			{"Enter", "details"},
		}
	case screenProfiles:
		return []hintRow{
			{"Z", "new profile"},
			{"E", "edit profile"},
			{"X", "delete profile"},
			{"Enter", "details"},
		}
	case screenLaunch:
		return []hintRow{
			{"W/S", "select profile"},
			{"Enter", "launch preview"},
		}
	case screenDoctor:
		return []hintRow{
			{"R", "run doctor"},
			{"D", "restore default providers"},
		}
	default:
		return []hintRow{
			{"1-8", "screens"},
			{"Tab", "focus"},
			{"Q", "quit"},
		}
	}
}

// contentForeground renders the active screen's content.
func (m *model) contentForeground(s *Styles) string {
	// Wizard overlay takes precedence over the active screen.
	if m.wizard.active {
		return m.wizardView(s)
	}
	switch m.active {
	case screenDashboard:
		return m.dashboardView(s)
	case screenProviders:
		if m.modelCatalog.active {
			return m.modelCatalogView(s)
		}
		return m.providersView(s)
	case screenKeys:
		return m.keysView(s)
	case screenProfiles:
		return m.profilesView(s)
	case screenLaunch:
		return m.launchView(s)
	case screenDoctor:
		return m.doctorView(s)
	case screenAudit:
		return m.auditView(s)
	case screenSettings:
		return m.settingsView(s)
	case screenScratch:
		return m.scratchView(s)
	case screenHelp:
		return m.helpView(s)
	}
	return ""
}

// footerText builds the status/toast line.
func (m *model) footerText() string {
	if m.statusMsg != "" {
		return m.statusMsg
	}
	return "W/S move | A/D pane | Enter open | Z add | E edit | X delete | R doctor | 1-9 screens | Tab focus | ? help | Q quit"
}

func padRightHint(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
