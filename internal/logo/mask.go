package logo

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"sync"
)

// Asset describes the source image and terminal-cell target size for a logo.
type Asset struct {
	ID     string
	Path   string
	Width  int
	Height int
	Crop   Crop
}

// Crop identifies a pixel region within a larger logo sheet.
type Crop struct {
	X int
	Y int
	W int
	H int
}

// Mask is a terminal-cell logo silhouette sampled from a real logo asset.
type Mask struct {
	ID     string
	Width  int
	Height int
	Cells  [][]float64
}

var (
	imageCacheMu sync.Mutex
	imageCache   = map[string]image.Image{}
)

// LoadLogoMask converts a raster logo asset into a terminal-cell mask.
func LoadLogoMask(path string, w, h int) (Mask, error) {
	if w <= 0 || h <= 0 {
		return Mask{}, fmt.Errorf("invalid logo mask size %dx%d", w, h)
	}

	f, err := os.Open(path)
	if err != nil {
		return Mask{}, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return Mask{}, err
	}
	return ImageToMask(img, w, h), nil
}

// LoadAssetMask converts a logo asset, optionally cropped from a sheet, into a mask.
func LoadAssetMask(asset Asset, path string) (Mask, error) {
	if asset.Width <= 0 || asset.Height <= 0 {
		return Mask{}, fmt.Errorf("invalid logo mask size %dx%d", asset.Width, asset.Height)
	}

	img, err := loadImageCached(path)
	if err != nil {
		return Mask{}, err
	}
	if asset.Crop.W > 0 && asset.Crop.H > 0 {
		img = cropImage(img, asset.Crop)
	}
	mask := ImageToMask(img, asset.Width, asset.Height)
	mask.ID = asset.ID
	return mask, nil
}

func loadImageCached(path string) (image.Image, error) {
	imageCacheMu.Lock()
	if img, ok := imageCache[path]; ok {
		imageCacheMu.Unlock()
		return img, nil
	}
	imageCacheMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	imageCacheMu.Lock()
	imageCache[path] = img
	imageCacheMu.Unlock()
	return img, nil
}

func cropImage(img image.Image, crop Crop) image.Image {
	b := img.Bounds()
	r := image.Rect(crop.X, crop.Y, crop.X+crop.W, crop.Y+crop.H).Intersect(b)
	if r.Empty() {
		return img
	}
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(r)
	}

	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			out.Set(x-r.Min.X, y-r.Min.Y, img.At(x, y))
		}
	}
	return out
}

// ImageToMask samples an image into a terminal-cell mask while preserving aspect.
// For opaque-background images (e.g. 3D renders with glow/bloom effects) it
// uses a local adaptive background: for each cell the background estimate is
// the minimum mean-luminance in a 3×3 neighbourhood of cells, not the global
// image corner. This isolates logo edges cleanly even against a soft glow.
func ImageToMask(img image.Image, w, h int) Mask {
	mask := Mask{Width: w, Height: h, Cells: make([][]float64, h)}
	for y := range mask.Cells {
		mask.Cells[y] = make([]float64, w)
	}
	if img == nil || w <= 0 || h <= 0 {
		return mask
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return mask
	}

	img = cropToForeground(img)
	bounds = img.Bounds()
	srcW = bounds.Dx()
	srcH = bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return mask
	}

	scale := math.Min(float64(w)/float64(srcW), float64(h)/float64(srcH))
	fitW := max(1, int(math.Round(float64(srcW)*scale)))
	fitH := max(1, int(math.Round(float64(srcH)*scale)))
	offX := (w - fitW) / 2
	offY := (h - fitH) / 2
	_, bgAlpha := estimateBackground(img)

	// Build per-cell source coordinate table and mean-luminance grid.
	type cellBox struct{ x0, y0, x1, y1 int }
	boxes := make([][]cellBox, h)
	meanLum := make([][]float64, h)
	for y := 0; y < h; y++ {
		boxes[y] = make([]cellBox, w)
		meanLum[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			if x < offX || x >= offX+fitW || y < offY || y >= offY+fitH {
				continue
			}
			sx0 := bounds.Min.X + int(math.Floor(float64(x-offX)*float64(srcW)/float64(fitW)))
			sx1 := bounds.Min.X + int(math.Ceil(float64(x-offX+1)*float64(srcW)/float64(fitW)))
			sy0 := bounds.Min.Y + int(math.Floor(float64(y-offY)*float64(srcH)/float64(fitH)))
			sy1 := bounds.Min.Y + int(math.Ceil(float64(y-offY+1)*float64(srcH)/float64(fitH)))
			boxes[y][x] = cellBox{sx0, sy0, sx1, sy1}
			meanLum[y][x] = meanLumRegion(img, sx0, sy0, sx1, sy1)
		}
	}

	// Per-cell local background = minimum mean-lum in a 3×3 neighbourhood.
	localBg := make([][]float64, h)
	const bgRadius = 3
	for y := 0; y < h; y++ {
		localBg[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			mn := 1.0
			for dy := -bgRadius; dy <= bgRadius; dy++ {
				for dx := -bgRadius; dx <= bgRadius; dx++ {
					ny2, nx2 := y+dy, x+dx
					if ny2 < 0 || ny2 >= h || nx2 < 0 || nx2 >= w {
						mn = 0
						continue
					}
					if v := meanLum[ny2][nx2]; v < mn {
						mn = v
					}
				}
			}
			localBg[y][x] = mn
		}
	}

	// Final pass: sample each cell against its local background reference.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < offX || x >= offX+fitW || y < offY || y >= offY+fitH {
				continue
			}
			cb := boxes[y][x]
			mask.Cells[y][x] = sampleRegion(img, cb.x0, cb.y0, cb.x1, cb.y1, localBg[y][x], bgAlpha)
		}
	}
	enhanceMaskDetail(mask.Cells)
	return mask
}

func cropToForeground(img image.Image) image.Image {
	b := img.Bounds()
	if b.Empty() {
		return img
	}

	bg := estimateBackgroundColor(img)
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !isForegroundPixel(img.At(x, y), bg) {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return img
	}

	padX := max(4, (maxX-minX+1)/24)
	padY := max(4, (maxY-minY+1)/18)
	r := image.Rect(minX-padX, minY-padY, maxX+padX+1, maxY+padY+1).Intersect(b)
	if r.Empty() || r == b {
		return img
	}
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(r)
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			out.Set(x-r.Min.X, y-r.Min.Y, img.At(x, y))
		}
	}
	return out
}

type rgbaFloat struct {
	r, g, b, a float64
	lum        float64
}

func estimateBackgroundColor(img image.Image) rgbaFloat {
	b := img.Bounds()
	points := [][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	var bg rgbaFloat
	for _, p := range points {
		c := colorToRGBAFloat(img.At(p[0], p[1]))
		bg.r += c.r
		bg.g += c.g
		bg.b += c.b
		bg.a += c.a
		bg.lum += c.lum
	}
	n := float64(len(points))
	bg.r /= n
	bg.g /= n
	bg.b /= n
	bg.a /= n
	bg.lum /= n
	return bg
}

func isForegroundPixel(c color.Color, bg rgbaFloat) bool {
	p := colorToRGBAFloat(c)
	if bg.a <= 0.10 {
		return p.a > 0.08
	}
	if math.Abs(p.a-bg.a) > 0.20 {
		return true
	}
	dr := p.r - bg.r
	dg := p.g - bg.g
	db := p.b - bg.b
	dist := math.Sqrt(dr*dr + dg*dg + db*db)
	lumDist := math.Abs(p.lum - bg.lum)
	return dist > 0.075 || lumDist > 0.055
}

func colorToRGBAFloat(c color.Color) rgbaFloat {
	r, g, b, a := c.RGBA()
	p := rgbaFloat{
		r: float64(r) / 65535.0,
		g: float64(g) / 65535.0,
		b: float64(b) / 65535.0,
		a: float64(a) / 65535.0,
	}
	p.lum = 0.2126*p.r + 0.7152*p.g + 0.0722*p.b
	return p
}

// meanLumRegion computes the average luminance of all pixels in a source region.
func meanLumRegion(img image.Image, x0, y0, x1, y1 int) float64 {
	b := img.Bounds()
	x0 = clampIntLocal(x0, b.Min.X, b.Max.X)
	x1 = clampIntLocal(x1, b.Min.X, b.Max.X)
	y0 = clampIntLocal(y0, b.Min.Y, b.Max.Y)
	y1 = clampIntLocal(y1, b.Min.Y, b.Max.Y)
	if x1 <= x0 {
		x1 = min(b.Max.X, x0+1)
	}
	if y1 <= y0 {
		y1 = min(b.Max.Y, y0+1)
	}
	var total float64
	var count int
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			l, _ := luminanceAlpha(img.At(x, y))
			total += l
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func sampleRegion(img image.Image, x0, y0, x1, y1 int, bgLum, bgAlpha float64) float64 {
	b := img.Bounds()
	x0 = clampIntLocal(x0, b.Min.X, b.Max.X)
	x1 = clampIntLocal(x1, b.Min.X, b.Max.X)
	y0 = clampIntLocal(y0, b.Min.Y, b.Max.Y)
	y1 = clampIntLocal(y1, b.Min.Y, b.Max.Y)
	if x1 <= x0 {
		x1 = min(b.Max.X, x0+1)
	}
	if y1 <= y0 {
		y1 = min(b.Max.Y, y0+1)
	}

	// Collect per-pixel strengths.
	var strengths []float64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			lum, alpha := luminanceAlpha(img.At(x, y))
			s := silhouetteStrength(lum, alpha, bgLum, bgAlpha)
			strengths = append(strengths, s)
		}
	}
	if len(strengths) == 0 {
		return 0
	}

	// Soft-max pooling: sort and blend top-20% with mean.
	// This ensures thin bright lines / sparse logo pixels score high even
	// when most of the cell is dark background.
	sortFloats(strengths)
	n := len(strengths)
	p80start := n * 4 / 5 // index where top-20% begins
	var topSum, allSum float64
	for i, v := range strengths {
		allSum += v
		if i >= p80start {
			topSum += v
		}
	}
	topCount := n - p80start
	if topCount <= 0 {
		topCount = 1
	}
	topMean := topSum / float64(topCount)
	allMean := allSum / float64(n)
	// Weight mostly toward bright peaks so thin strokes do not get averaged
	// into the surrounding background.
	return clampFloatLocal(topMean*0.90+allMean*0.10, 0, 1)
}

func enhanceMaskDetail(cells [][]float64) {
	if len(cells) == 0 || len(cells[0]) == 0 {
		return
	}
	src := make([][]float64, len(cells))
	for y := range cells {
		src[y] = append([]float64(nil), cells[y]...)
	}

	for y := range cells {
		for x := range cells[y] {
			v := src[y][x]
			if v <= 0 {
				continue
			}
			near := localAverage(src, x, y, 1)
			wider := localAverage(src, x, y, 2)
			edge := clampFloatLocal(v-wider, -0.25, 0.65)
			contrast := v + edge*0.72 + (v-near)*0.34
			if contrast < 0.035 {
				cells[y][x] = 0
				continue
			}
			if contrast < 0.095 && localMax(src, x, y, 1) < 0.16 {
				cells[y][x] = 0
				continue
			}
			cells[y][x] = clampFloatLocal(math.Pow(clampFloatLocal(contrast, 0, 1), 0.78), 0, 1)
		}
	}
}

func localAverage(cells [][]float64, x, y, radius int) float64 {
	var total float64
	var count int
	for dy := -radius; dy <= radius; dy++ {
		yy := y + dy
		if yy < 0 || yy >= len(cells) {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			xx := x + dx
			if xx < 0 || xx >= len(cells[yy]) {
				continue
			}
			total += cells[yy][xx]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func localMax(cells [][]float64, x, y, radius int) float64 {
	mx := 0.0
	for dy := -radius; dy <= radius; dy++ {
		yy := y + dy
		if yy < 0 || yy >= len(cells) {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			xx := x + dx
			if xx < 0 || xx >= len(cells[yy]) {
				continue
			}
			if cells[yy][xx] > mx {
				mx = cells[yy][xx]
			}
		}
	}
	return mx
}

// sortFloats sorts a float64 slice in ascending order (insertion sort for small n).
func sortFloats(a []float64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

func silhouetteStrength(lum, alpha, bgLum, bgAlpha float64) float64 {
	if alpha <= 0.02 {
		return 0
	}
	// Transparent-background images: use alpha as primary signal.
	if bgAlpha <= 0.10 {
		return math.Pow(clampFloatLocal(alpha, 0, 1), 0.55)
	}
	// Opaque-background (RGB) images: strength = contrast vs background.
	// Dead zone below 0.08 contrast eliminates dark floor reflections / JPEG
	// noise while preserving real logo content (lum ≥ 0.15 on dark bg).
	contrast := math.Abs(lum - bgLum)
	if contrast < 0.08 {
		return 0
	}
	// Map [0.08, 1.0] → [0, 1] with a mild gamma lift for midtones.
	scaled := (contrast - 0.08) / 0.92
	return alpha * math.Pow(clampFloatLocal(scaled, 0, 1), 0.55)
}

func estimateBackground(img image.Image) (float64, float64) {
	b := img.Bounds()
	points := [][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	var lumTotal, alphaTotal float64
	for _, p := range points {
		lum, alpha := luminanceAlpha(img.At(p[0], p[1]))
		lumTotal += lum
		alphaTotal += alpha
	}
	return lumTotal / float64(len(points)), alphaTotal / float64(len(points))
}

func luminanceAlpha(c color.Color) (float64, float64) {
	r, g, b, a := c.RGBA()
	alpha := float64(a) / 65535.0
	if alpha <= 0 {
		return 0, 0
	}
	rf := float64(r) / 65535.0
	gf := float64(g) / 65535.0
	bf := float64(b) / 65535.0
	return 0.2126*rf + 0.7152*gf + 0.0722*bf, alpha
}

func clampIntLocal(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloatLocal(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
