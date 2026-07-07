package tui

import (
	"math"
	"math/rand"
	"time"

	tea "charm.land/bubbletea/v2"
)

// matrixMsg advances the animation.
type matrixMsg struct{}

// tickCmd schedules the next animation frame.
func tickCmd() tea.Cmd {
	return tea.Tick(85*time.Millisecond, func(_ time.Time) tea.Msg {
		return matrixMsg{}
	})
}

// matrixSemantic identifies event-driven spark types.
type matrixSemantic int

const (
	matrixNormal matrixSemantic = iota
	matrixUnlock
	matrixAddKey
	matrixLaunch
	matrixDoctorOK
	matrixDoctorFail
)

// Matrix renders a rolling, morphing, alive Matrix field.
type Matrix struct {
	Width  int
	Height int
	Frame  int

	Drops []MatrixDrop
	Waves []MatrixWave

	// waveCache stores pre-calculated wave field intensities for the frame.
	waveCache [][]float64

	// semanticEvents holds transient event sparks that decay over frames.
	semanticEvents []semanticEvent

	RNG *rand.Rand
}

type semanticEvent struct {
	x, y      int
	semantic  matrixSemantic
	lifetime  int
	intensity float64
}

type MatrixDrop struct {
	X          int
	Y          float64
	Speed      float64
	Length     int
	Glyphs     []rune
	Base       float64
	Phase      float64
	Layer      int
	SparkTTL   int
	DriftPhase float64
}

type MatrixWave struct {
	Phase     float64
	Speed     float64
	XFreq     float64
	YFreq     float64
	Amplitude float64
	Direction float64
}

var glyphSet = []rune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz+-*/=<>:;|_#$%&~^@{}[]")

// matrixPalette is the color ramp from deep background to bright head.
var matrixPalette = []string{
	"#080012",
	"#16002e",
	"#2a1a4a",
	"#4a2d7a",
	"#6a3fb0",
	"#9b6dff",
	"#c79cff",
	"#ffffff",
}

// sparkColors maps semantic events to their spark colors.
var sparkColors = map[matrixSemantic]string{
	matrixUnlock:     "#39ff14",
	matrixAddKey:     "#c79cff",
	matrixLaunch:     "#ffffff",
	matrixDoctorOK:   "#39ff14",
	matrixDoctorFail: "#ff3b5c",
}

// NewMatrix creates a Matrix field.
func NewMatrix(w, h int) *Matrix {
	m := &Matrix{
		RNG: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	m.initWaves()
	m.Resize(w, h)
	return m
}

func (m *Matrix) initWaves() {
	m.Waves = []MatrixWave{
		{Phase: m.RNG.Float64() * math.Pi * 2, Speed: 0.020, XFreq: 0.050, YFreq: 0.085, Amplitude: 0.55, Direction: 1},
		{Phase: m.RNG.Float64() * math.Pi * 2, Speed: 0.013, XFreq: 0.085, YFreq: -0.045, Amplitude: 0.38, Direction: -1},
		{Phase: m.RNG.Float64() * math.Pi * 2, Speed: 0.008, XFreq: 0.028, YFreq: 0.033, Amplitude: 0.34, Direction: 1},
		{Phase: m.RNG.Float64() * math.Pi * 2, Speed: 0.017, XFreq: 0.135, YFreq: 0.018, Amplitude: 0.22, Direction: -1},
	}
}

// Resize updates dimensions and dynamically adjusts drop counts without
// destroying existing on-screen streams.
func (m *Matrix) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if m.Width == w && m.Height == h {
		return
	}

	m.Width, m.Height = w, h

	if m.Width <= 0 || m.Height <= 0 {
		m.Drops = nil
		m.waveCache = nil
		return
	}

	count := int(float64(m.Width) * 1.55)
	if m.Height > 42 {
		count += m.Width / 3
	}
	if count < 72 {
		count = 72
	}
	if count > 520 {
		count = 520
	}

	if len(m.Drops) < count {
		for i := len(m.Drops); i < count; i++ {
			m.Drops = append(m.Drops, m.newDrop(true))
		}
	} else if len(m.Drops) > count {
		m.Drops = m.Drops[:count]
	}

	m.allocateWaveCache()
}

func (m *Matrix) allocateWaveCache() {
	m.waveCache = make([][]float64, m.Height)
	for y := range m.waveCache {
		m.waveCache[y] = make([]float64, m.Width)
	}
}

func (m *Matrix) newDrop(randomY bool) MatrixDrop {
	h := maxInt(m.Height, 1)

	layerRoll := m.RNG.Float64()
	layer := 1
	switch {
	case layerRoll < 0.18:
		layer = 0
	case layerRoll > 0.78:
		layer = 2
	}

	lengthMin, lengthMax := 6, maxInt(h/2, 12)
	if layer == 0 {
		lengthMin, lengthMax = 3, maxInt(h/4, 8)
	} else if layer == 2 {
		lengthMin, lengthMax = 9, maxInt((h*2)/3, 16)
	}

	length := lengthMin + m.RNG.Intn(maxInt(lengthMax-lengthMin+1, 1))

	var y float64
	if randomY {
		y = float64(m.RNG.Intn(h+length*2)) - float64(length*2)
	} else {
		y = -float64(length) - float64(m.RNG.Intn(maxInt(h/2, 8)))
	}

	speed, base := 0.30+m.RNG.Float64()*0.62, 0.38+m.RNG.Float64()*0.45
	if layer == 0 {
		speed, base = 0.12+m.RNG.Float64()*0.26, 0.16+m.RNG.Float64()*0.28
	} else if layer == 2 {
		speed, base = 0.42+m.RNG.Float64()*0.78, 0.52+m.RNG.Float64()*0.42
	}

	d := MatrixDrop{
		X:          m.RNG.Intn(maxInt(m.Width, 1)),
		Y:          y,
		Speed:      speed,
		Length:     length,
		Glyphs:     make([]rune, length),
		Base:       base,
		Phase:      m.RNG.Float64() * math.Pi * 2,
		Layer:      layer,
		DriftPhase: m.RNG.Float64() * math.Pi * 2,
	}

	for i := range d.Glyphs {
		d.Glyphs[i] = glyphSet[m.RNG.Intn(len(glyphSet))]
	}
	return d
}

// TriggerSpark adds a semantic event spark at a random position.
func (m *Matrix) TriggerSpark(semantic matrixSemantic) {
	if m.Width <= 0 || m.Height <= 0 {
		return
	}
	e := semanticEvent{
		x:         m.RNG.Intn(m.Width),
		y:         m.RNG.Intn(m.Height),
		semantic:  semantic,
		lifetime:  30 + m.RNG.Intn(30),
		intensity: 1.0,
	}
	m.semanticEvents = append(m.semanticEvents, e)
}

// Update advances the animation and precomputes the wave field to save CPU.
func (m *Matrix) Update(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case matrixMsg:
		m.Frame++

		for i := range m.Waves {
			m.Waves[i].Phase += m.Waves[i].Speed * m.Waves[i].Direction
		}

		m.updateWaveCache()

		for i := range m.Drops {
			d := &m.Drops[i]
			wave := m.waveAt(d.X, int(d.Y))
			pulse := 0.5 + 0.5*math.Sin(float64(m.Frame)*0.055+d.Phase)
			d.Y += d.Speed * (0.82 + wave*0.48 + pulse*0.12)

			mutateChance := 18
			if wave > 0.72 {
				mutateChance = 8
			}
			if d.Layer == 2 && wave > 0.62 {
				mutateChance = 5
			}

			if m.RNG.Intn(mutateChance) == 0 {
				d.Glyphs[m.RNG.Intn(len(d.Glyphs))] = glyphSet[m.RNG.Intn(len(glyphSet))]
			}

			if d.SparkTTL > 0 {
				d.SparkTTL--
			} else if d.Layer == 2 && wave > 0.78 && m.RNG.Intn(32) == 0 {
				d.SparkTTL = 2 + m.RNG.Intn(4)
			}

			if int(d.Y)-d.Length > m.Height {
				m.Drops[i] = m.newDrop(false)
			}
		}

		// Decay semantic events.
		alive := m.semanticEvents[:0]
		for _, e := range m.semanticEvents {
			e.lifetime--
			e.intensity *= 0.94
			if e.lifetime > 0 && e.intensity > 0.05 {
				alive = append(alive, e)
			}
		}
		m.semanticEvents = alive

		return tickCmd()
	}
	return nil
}

// updateWaveCache calculates the field once per frame.
func (m *Matrix) updateWaveCache() {
	if len(m.waveCache) != m.Height || (m.Height > 0 && len(m.waveCache[0]) != m.Width) {
		m.allocateWaveCache()
	}
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			m.waveCache[y][x] = m.calculateWaveAt(x, y)
		}
	}
}

// calculateWaveAt performs the heavy math.
func (m *Matrix) calculateWaveAt(x, y int) float64 {
	if len(m.Waves) == 0 {
		return 0
	}
	total, amp := 0.0, 0.0

	for _, w := range m.Waves {
		v := math.Sin(float64(x)*w.XFreq + float64(y)*w.YFreq + w.Phase)
		total += v * w.Amplitude
		amp += math.Abs(w.Amplitude)
	}

	if amp <= 0 {
		return 0
	}
	n := (total/amp + 1.0) * 0.5
	n = n * n * (3 - 2*n)

	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}

// waveAt performs a fast 2D slice lookup.
func (m *Matrix) waveAt(x, y int) float64 {
	if y >= 0 && y < len(m.waveCache) && x >= 0 && x < len(m.waveCache[y]) {
		return m.waveCache[y][x]
	}
	return 0
}

// Render fills a buffer of matrixCell cells.
func (m *Matrix) Render(buf *MatrixBuffer) {
	if m.Width <= 0 || m.Height <= 0 || buf == nil {
		return
	}

	m.renderAmbientHaze(buf)

	for _, d := range m.Drops {
		drift := 0
		if d.Layer == 2 {
			driftWave := math.Sin(float64(m.Frame)*0.033 + d.DriftPhase)
			if driftWave > 0.58 {
				drift = 1
			} else if driftWave < -0.58 {
				drift = -1
			}
		}

		x := d.X + drift
		if x < 0 || x >= m.Width {
			continue
		}

		for t := 0; t < d.Length; t++ {
			y := int(d.Y) - t
			if y < 0 || y >= m.Height {
				continue
			}

			trailFrac := float64(t) / float64(maxInt(d.Length-1, 1))
			trailFade := math.Exp(-trailFrac * 2.15)
			wave := m.waveAt(x, y)
			pulse := 0.5 + 0.5*math.Sin(float64(m.Frame)*0.040+d.Phase+float64(t)*0.22)
			noise := stableNoise(x, y, m.Frame/3)

			intensity := (d.Base*0.38 + wave*0.48 + pulse*0.10 + noise*0.06) * trailFade

			if d.Layer == 0 {
				intensity *= 0.52
			} else if d.Layer == 2 {
				intensity *= 1.16
			}

			if t > d.Length*2/3 {
				intensity *= 0.72
			}
			if intensity < 0.105 {
				continue
			}
			if intensity > 1 {
				intensity = 1
			}

			spark := d.SparkTTL > 0 && t == 0
			colorIdx := intensityToColorAlive(intensity, t, d.Layer, spark, wave)

			glyph := d.Glyphs[t%len(d.Glyphs)]
			if stableNoise(x+13, y+7, m.Frame/2) > 0.965 && t < 3 {
				glyph = glyphSet[int(stableNoise(x, y, m.Frame)*float64(len(glyphSet)))%len(glyphSet)]
			}

			putMatrixCell(buf, x, y, glyph, colorIdx, spark)

			if d.Layer == 2 && t > 0 && t < d.Length-2 && stableNoise(x+31, y+19, m.Frame/3) > 0.82 {
				echoX := x - 1
				if stableNoise(x+7, y+23, m.Frame/5) > 0.5 {
					echoX = x + 1
				}
				echoColor := colorIdx - 1
				if echoColor < 2 {
					echoColor = 2
				}
				putMatrixCell(buf, echoX, y, glyph, echoColor, false)
			}
		}
	}

	// Render semantic event sparks.
	for _, e := range m.semanticEvents {
		if e.x < 0 || e.x >= m.Width || e.y < 0 || e.y >= m.Height {
			continue
		}
		radius := int(math.Ceil(e.intensity * 4))
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				sx, sy := e.x+dx, e.y+dy
				if sx < 0 || sx >= m.Width || sy < 0 || sy >= m.Height {
					continue
				}
				dist := math.Sqrt(float64(dx*dx + dy*dy))
				if dist > float64(radius) {
					continue
				}
				falloff := 1.0 - dist/float64(maxInt(radius, 1))
				if falloff < 0 {
					falloff = 0
				}
				brightness := int(falloff * float64(len(matrixPalette)-1))
				if brightness >= len(matrixPalette) {
					brightness = len(matrixPalette) - 1
				}
				// Higher brightness (near spark center) -> brighter palette color.
				// The palette ramps dark (index 0) to bright (last index), so map
				// brightness directly to a high index.
				idx := 1 + brightness
				if idx >= len(matrixPalette) {
					idx = len(matrixPalette) - 1
				}
				existing := buf.CellAt(sx, sy)
				if idx > existing.colorIdx {
					buf.setCell(sx, sy, '*', idx, true)
				}
			}
		}
	}
}

func (m *Matrix) renderAmbientHaze(buf *MatrixBuffer) {
	if m.Width <= 0 || m.Height <= 0 {
		return
	}

	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			wave := m.waveAt(x, y)
			n := stableNoise(x, y, m.Frame/4)

			if n < 0.952-wave*0.085 {
				continue
			}

			intensity := 0.12 + wave*0.36 + stableNoise(x+5, y+11, m.Frame/6)*0.10
			if intensity < 0.16 {
				continue
			}
			if intensity > 0.48 {
				intensity = 0.48
			}

			colorIdx := 2
			if intensity > 0.40 {
				colorIdx = 3
			}

			glyphIdx := int(stableNoise(x+17, y+29, m.Frame/5) * float64(len(glyphSet)))
			if glyphIdx < 0 {
				glyphIdx = 0
			}
			if glyphIdx >= len(glyphSet) {
				glyphIdx = len(glyphSet) - 1
			}

			putMatrixCell(buf, x, y, glyphSet[glyphIdx], colorIdx, false)
		}
	}
}

func putMatrixCell(buf *MatrixBuffer, x, y int, glyph rune, colorIdx int, spark bool) {
	if buf == nil || y < 0 || y >= buf.Height || x < 0 || x >= buf.Width {
		return
	}
	if colorIdx > buf.cells[y][x].colorIdx {
		buf.cells[y][x] = matrixCell{ch: glyph, colorIdx: colorIdx, spark: spark}
	}
}

func intensityToColorAlive(intensity float64, trailPos int, layer int, spark bool, wave float64) int {
	if spark {
		return 7
	}
	if trailPos == 0 && layer == 2 && intensity > 0.90 && wave > 0.80 {
		return 6
	}

	if trailPos == 0 {
		switch {
		case intensity > 0.74:
			return 5
		case intensity > 0.44:
			return 4
		case intensity > 0.25:
			return 3
		default:
			return 2
		}
	}

	switch {
	case intensity > 0.78:
		return 5
	case intensity > 0.52:
		return 4
	case intensity > 0.29:
		return 3
	case intensity > 0.15:
		return 2
	default:
		return 1
	}
}

func stableNoise(x, y, frame int) float64 {
	n := uint32(x*73856093 ^ y*19349663 ^ frame*83492791)
	n ^= n << 13
	n ^= n >> 17
	n ^= n << 5
	return float64(n%1000) / 1000.0
}

type matrixCell struct {
	ch       rune
	colorIdx int
	spark    bool
}

// MatrixBuffer is the target for Matrix.Render.
type MatrixBuffer struct {
	Width     int
	Height    int
	cells     [][]matrixCell
	Protected []Rect
}

// Rect is an axis-aligned rectangle in screen space.
type Rect struct {
	X, Y, W, H int
}

// NewMatrixBuffer creates a buffer of the given size.
func NewMatrixBuffer(w, h int) *MatrixBuffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	cells := make([][]matrixCell, h)
	for y := range cells {
		cells[y] = make([]matrixCell, w)
	}

	return &MatrixBuffer{Width: w, Height: h, cells: cells}
}

// Clear resets the buffer.
func (b *MatrixBuffer) Clear() {
	for y := range b.cells {
		for x := range b.cells[y] {
			b.cells[y][x] = matrixCell{}
		}
	}
	b.Protected = nil
}

// Protect adds a protected rectangle.
func (b *MatrixBuffer) Protect(r Rect) {
	b.Protected = append(b.Protected, r)
}

// CellAt returns the matrix cell at (x,y), or empty if protected.
func (b *MatrixBuffer) CellAt(x, y int) matrixCell {
	if b == nil || y < 0 || y >= b.Height || x < 0 || x >= b.Width {
		return matrixCell{}
	}
	for _, r := range b.Protected {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return matrixCell{}
		}
	}
	return b.cells[y][x]
}

// setCell writes directly to the buffer matrix without priority checks.
func (b *MatrixBuffer) setCell(x, y int, ch rune, colorIdx int, spark bool) {
	if x < 0 || x >= b.Width || y < 0 || y >= b.Height {
		return
	}
	b.cells[y][x] = matrixCell{ch: ch, colorIdx: colorIdx, spark: spark}
}

func intensityToColor(intensity float64, trailPos int, spark bool) int {
	return intensityToColorAlive(intensity, trailPos, 1, spark, intensity)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
