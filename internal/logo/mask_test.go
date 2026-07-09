package logo

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImageToMask_SamplesTransparentLogoSilhouette(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 4; y < 16; y++ {
		for x := 4; x < 16; x++ {
			if x == y || x == 19-y || (x >= 8 && x <= 11) {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}

	mask := ImageToMask(img, 10, 10)
	if mask.Width != 10 || mask.Height != 10 {
		t.Fatalf("mask size = %dx%d, want 10x10", mask.Width, mask.Height)
	}

	var active, blank int
	for y := range mask.Cells {
		for x := range mask.Cells[y] {
			if mask.Cells[y][x] > 0.25 {
				active++
			}
			if mask.Cells[y][x] == 0 {
				blank++
			}
		}
	}
	if active == 0 {
		t.Fatal("expected sampled mask to contain active logo cells")
	}
	if blank == 0 {
		t.Fatal("expected transparent background to remain outside mask")
	}
}

func TestLoadLogoMask_DecodesPNGAsset(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 1; y < 7; y++ {
		for x := 1; x < 7; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}

	path := filepath.Join(t.TempDir(), "logo.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	mask, err := LoadLogoMask(path, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if mask.Cells[3][3] <= 0.5 {
		t.Fatalf("expected center of PNG logo to be active, got %.2f", mask.Cells[3][3])
	}
}

func TestLoadDefaultMask_HermesAgentAsset(t *testing.T) {
	mask, ok := LoadDefaultMask("hermes")
	if !ok {
		t.Fatal("expected hermes logo asset to be available")
	}
	if mask.Width != 176 || mask.Height != 96 {
		t.Fatalf("hermes mask size = %dx%d, want 176x96", mask.Width, mask.Height)
	}
}

func TestEnhanceMaskDetail_DropsLowHazeKeepsStrokes(t *testing.T) {
	cells := make([][]float64, 12)
	for y := range cells {
		cells[y] = make([]float64, 24)
		for x := range cells[y] {
			cells[y][x] = 0.055
		}
	}
	for x := 5; x < 19; x++ {
		cells[5][x] = 0.42
	}
	for y := 3; y < 9; y++ {
		cells[y][8] = 0.46
		cells[y][15] = 0.46
	}

	enhanceMaskDetail(cells)

	if cells[0][0] != 0 {
		t.Fatalf("expected low blended haze to drop out, got %.3f", cells[0][0])
	}
	if cells[5][8] <= 0.45 {
		t.Fatalf("expected thin stroke detail to stay strong, got %.3f", cells[5][8])
	}
}
