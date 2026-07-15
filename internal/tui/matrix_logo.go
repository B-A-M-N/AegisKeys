package tui

import (
	"math"
	"math/rand"
	"strings"

	"aegiskeys/internal/logo"
	"github.com/charmbracelet/harmonica"
)

type matrixLogoReveal struct {
	id       string
	mask     logo.Mask
	hasMask  bool
	assetOK  bool
	value    float64
	velocity float64
	target   float64
	spring   harmonica.Spring
	xNorm    float64
	yNorm    float64
	phase    float64
	hold     int
}

// logoRotateFrames controls how many animation frames each logo is shown.
const logoRotateFrames = 120

func (m *Matrix) initLogoReveals() {
	ids := []string{
		"aider", "crush", "qwen", "goose", "cline", "claude", "free-claude", "hermes", "vibe", "codex",
		"mimo", "opencode", "openhands", "gemini", "copilot", "continue", "zed", "intellij",
		"openai", "anthropic", "deepseek", "mistral", "nvidia", "google", "moonshotai", "z-ai",
	}
	m.logos = make([]matrixLogoReveal, 0, len(ids))
	for i, id := range ids {
		m.logos = append(m.logos, matrixLogoReveal{
			id:      id,
			assetOK: logo.DefaultAssetAvailable(id),
			target:  0.04,
			spring:  harmonica.NewSpring(harmonica.FPS(12), 7.0, 0.55),
			xNorm:   0.04 + math.Mod(float64(i)*0.382, 0.92),
			yNorm:   0.06 + math.Mod(float64(i)*0.618, 0.86),
			phase:   float64(i) * 0.73,
			hold:    8 + i%11,
		})
	}
	// Build the initial shuffle deck.
	m.rebuildShuffleDeck()
}

func (m *Matrix) SetLogo(id string) {
	if m == nil {
		return
	}
	requested := normalizeLogoID(id)
	if m.logoAvailable(requested) {
		m.setFocusLogo(requested)
		m.ensureLogoVisible(requested)
		return
	}
	if m.logoAvailable(m.focusLogo) {
		m.ensureLogoVisible(m.focusLogo)
		return
	}
	m.setFocusLogo(m.rotatingLogoID())
	m.ensureLogoVisible(m.focusLogo)
}

func (m *Matrix) setFocusLogo(id string) {
	if id == "" || m.focusLogo == id {
		return
	}
	m.focusLogo = id
	m.focusLogoFrame = m.Frame
	for i := range m.logos {
		if m.logos[i].id == id {
			m.logos[i].value = minFloat(m.logos[i].value, 0.18)
			m.logos[i].velocity = 0
			return
		}
	}
}

func (m *Matrix) updateLogoReveals() {
	if len(m.logos) == 0 {
		m.initLogoReveals()
	}
	// Advance carousel on schedule.
	if !m.logoAvailable(m.focusLogo) || (m.focusLogo != "" && m.Frame%logoRotateFrames == 0) {
		m.setFocusLogo(m.nextShuffledLogoID())
	}
	for i := range m.logos {
		logo := &m.logos[i]
		if !logo.assetOK && !logo.hasMask {
			logo.target = 0
			logo.value, logo.velocity = logo.spring.Update(logo.value, logo.velocity, logo.target)
			continue
		}
		if logo.id == m.focusLogo {
			logo.target = 1.0
			logo.hold = 22
		} else {
			logo.hold = 0
			logo.target = 0
		}
		logo.value, logo.velocity = logo.spring.Update(logo.value, logo.velocity, logo.target)
	}
}

func (m *Matrix) logoAvailable(id string) bool {
	if id == "" {
		return false
	}
	for i := range m.logos {
		if m.logos[i].id == id && (m.logos[i].assetOK || m.logos[i].hasMask) {
			return true
		}
	}
	return false
}

// nextShuffledLogoID picks the next logo from a Fisher-Yates shuffled deck.
// When the deck is exhausted it is re-shuffled, guaranteeing every logo with
// a mask appears exactly once per cycle before any logo repeats.
func (m *Matrix) nextShuffledLogoID() string {
	// Pop from the front of the deck.
	for len(m.shuffleDeck) > 0 {
		id := m.shuffleDeck[0]
		m.shuffleDeck = m.shuffleDeck[1:]
		if m.logoAvailable(id) {
			return id
		}
	}
	// Deck empty — re-shuffle and try again.
	m.rebuildShuffleDeck()
	for _, id := range m.shuffleDeck {
		if m.logoAvailable(id) {
			return id
		}
	}
	return ""
}

// rebuildShuffleDeck builds a Fisher-Yates shuffled list of all logo IDs that
// have a loaded mask.
func (m *Matrix) rebuildShuffleDeck() {
	available := make([]string, 0, len(m.logos))
	for i := range m.logos {
		if m.logos[i].assetOK || m.logos[i].hasMask {
			available = append(available, m.logos[i].id)
		}
	}
	if len(available) == 0 {
		m.shuffleDeck = nil
		return
	}
	rng := m.RNG
	if rng == nil {
		rng = rand.New(rand.NewSource(42))
	}
	for i := len(available) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		available[i], available[j] = available[j], available[i]
	}
	m.shuffleDeck = available
}

// rotatingLogoID is kept for compatibility with SetLogo fallback paths.
func (m *Matrix) rotatingLogoID() string {
	available := 0
	for i := range m.logos {
		if m.logos[i].assetOK || m.logos[i].hasMask {
			available++
		}
	}
	if available == 0 {
		return ""
	}
	slot := (m.Frame / logoRotateFrames) % available
	for i := range m.logos {
		if !m.logos[i].assetOK && !m.logos[i].hasMask {
			continue
		}
		if slot == 0 {
			return m.logos[i].id
		}
		slot--
	}
	return ""
}

func (m *Matrix) ensureLogoVisible(id string) {
	if id == "" {
		return
	}
	for i := range m.logos {
		if m.logos[i].id == id && (m.logos[i].assetOK || m.logos[i].hasMask) && m.logos[i].value < 0.35 {
			m.logos[i].value = 0.35
			m.logos[i].target = 1
			return
		}
	}
}

func (m *Matrix) renderLogoSilhouettes(buf *MatrixBuffer) {
	if m == nil || buf == nil || m.Width < 80 || m.Height < 24 {
		return
	}
	if !m.logoAvailable(m.focusLogo) {
		m.setFocusLogo(m.rotatingLogoID())
		m.ensureLogoVisible(m.focusLogo)
	}
	for i := range m.logos {
		if m.logos[i].id != m.focusLogo {
			continue
		}
		if !m.ensureLogoMaskLoaded(&m.logos[i]) {
			return
		}
		m.renderLogoSilhouette(buf, &m.logos[i])
		return
	}
}

func (m *Matrix) ensureLogoMaskLoaded(reveal *matrixLogoReveal) bool {
	if reveal == nil {
		return false
	}
	if reveal.hasMask {
		return true
	}
	if !reveal.assetOK {
		return false
	}
	mask, ok := logo.LoadDefaultMask(reveal.id)
	if !ok {
		reveal.assetOK = false
		return false
	}
	reveal.mask = mask
	reveal.hasMask = true
	return true
}

func (m *Matrix) renderLogoSilhouette(buf *MatrixBuffer, logo *matrixLogoReveal) {
	if logo == nil || !logo.hasMask || logo.mask.Width <= 0 || logo.mask.Height <= 0 {
		return
	}
	reveal := clampFloat(logo.value, 0, 1.18)
	if reveal < 0.10 {
		return
	}

	panel := m.logoRevealPanel()
	renderW := maxInt(1, logo.mask.Width/2)
	renderH := maxInt(1, logo.mask.Height/4)
	contentW := maxInt(1, panel.W)
	contentH := maxInt(1, panel.H)
	if renderW > contentW {
		renderW = contentW
	}
	if renderH > contentH {
		renderH = contentH
	}
	startX := panel.X + (contentW-renderW)/2
	startY := panel.Y + (contentH-renderH)/2

	age := maxInt(0, m.Frame-m.focusLogoFrame)
	revealGate := logoRevealGate(age, renderW)
	for cy := 0; cy < renderH; cy++ {
		for cx := 0; cx < renderW; cx++ {
			x := startX + cx
			y := startY + cy
			if x < 0 || x >= buf.Width || y < 0 || y >= buf.Height-1 {
				continue
			}
			if x < panel.X || x >= panel.X+panel.W || y < panel.Y || y >= panel.Y+panel.H {
				continue
			}

			wave := m.waveAt(x, y)
			n := stableNoise(x+37+int(logo.phase*10), y+53, m.Frame/3)
			if float64(cx) > revealGate && n > 0.08+reveal*0.62 {
				continue
			}
			glyph, inside := logoBrailleGlyph(logo.mask, cx, cy, reveal, n, age)
			if inside <= 0.04 {
				continue
			}
			edge := logoEdgeAccent(logo.mask, cx, cy)
			front := logoSweepAccent(float64(cx), revealGate)
			strength := clampFloat(inside*reveal+edge*0.18+front*0.22+wave*0.06+n*0.02, 0, 1.22)

			colorIdx := logoRainColor(strength, reveal, wave, n)
			putMatrixCell(buf, x, y, glyph, colorIdx, false)
		}
	}
}

func (m *Matrix) logoRevealPanel() Rect {
	contentX := sidebarWidth
	contentW := maxInt(1, m.Width-contentX)
	panelW := maxInt(52, contentW-4)
	if contentW >= 88 {
		panelW = minIntLocal(92, contentW-4)
	}
	if panelW > contentW {
		panelW = contentW
	}
	panelH := minIntLocal(26, maxInt(20, (m.Height*3)/5))
	x := m.Width - panelW - 2
	minX := contentX + contentW/4
	if x < minX {
		x = minX
	}
	if x+panelW > m.Width-1 {
		x = maxInt(contentX, m.Width-panelW-1)
	}
	y := 2 + (m.Height-4-panelH)/2
	if y < 1 {
		y = 1
	}
	return Rect{X: x, Y: y, W: panelW, H: panelH}
}

func logoRevealGate(age, renderW int) float64 {
	if renderW <= 0 {
		return 0
	}
	materialize := 56
	if age < materialize {
		t := easeOutCubic(float64(age) / float64(materialize))
		return -8 + t*float64(renderW+14)
	}
	breathe := 0.5 + 0.5*math.Sin(float64(age-materialize)*0.045)
	return float64(renderW+8) + breathe*3
}

func logoSweepAccent(x, gate float64) float64 {
	d := math.Abs(x - gate)
	if d > 5 {
		return 0
	}
	return 1 - d/5
}

func easeOutCubic(t float64) float64 {
	t = clampFloat(t, 0, 1)
	u := 1 - t
	return 1 - u*u*u
}

func logoBrailleGlyph(mask logo.Mask, cellX, cellY int, reveal, noise float64, age int) (rune, float64) {
	dotBits := [4][2]int{
		{0x01, 0x08},
		{0x02, 0x10},
		{0x04, 0x20},
		{0x40, 0x80},
	}
	var bits int
	var total float64
	var count int
	settle := easeOutCubic(float64(age) / 48)
	threshold := 0.10 + (1-reveal)*0.13 + noise*0.025 - settle*0.025
	for dy := 0; dy < 4; dy++ {
		sy := cellY*4 + dy
		if sy < 0 || sy >= len(mask.Cells) {
			continue
		}
		row := mask.Cells[sy]
		for dx := 0; dx < 2; dx++ {
			sx := cellX*2 + dx
			if sx < 0 || sx >= len(row) {
				continue
			}
			v := row[sx]
			total += v
			count++
			if v >= threshold {
				bits |= dotBits[dy][dx]
			}
		}
	}
	if bits == 0 || count == 0 {
		return ' ', 0
	}
	return rune(0x2800 + bits), total / float64(count)
}

func logoEdgeAccent(mask logo.Mask, cellX, cellY int) float64 {
	center := logoCellMean(mask, cellX, cellY)
	if center <= 0 {
		return 0
	}
	maxDelta := 0.0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			d := math.Abs(center - logoCellMean(mask, cellX+dx, cellY+dy))
			if d > maxDelta {
				maxDelta = d
			}
		}
	}
	return clampFloat(maxDelta*1.4, 0, 1)
}

func logoCellMean(mask logo.Mask, cellX, cellY int) float64 {
	var total float64
	var count int
	for dy := 0; dy < 4; dy++ {
		sy := cellY*4 + dy
		if sy < 0 || sy >= len(mask.Cells) {
			continue
		}
		row := mask.Cells[sy]
		for dx := 0; dx < 2; dx++ {
			sx := cellX*2 + dx
			if sx < 0 || sx >= len(row) {
				continue
			}
			total += row[sx]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func logoRainColor(strength, reveal, wave, noise float64) int {
	intensity := strength*0.98 + reveal*0.18 + wave*0.08 + noise*0.02
	switch {
	case intensity > 0.84:
		return 6
	case intensity > 0.62:
		return 5
	default:
		return 4
	}
}

func minIntLocal(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeLogoID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "mistralai", "mistral-vibe":
		return "mistral"
	case "anthropic":
		return "anthropic"
	default:
		return id
	}
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
