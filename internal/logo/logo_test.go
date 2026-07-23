package logo

import (
	"image/color"
	"testing"
)

func TestMarkUsesRequestedColor(t *testing.T) {
	img := Mark(32, White)

	var painted int
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			pixel := img.NRGBAAt(x, y)
			if pixel.A == 0 {
				continue
			}
			painted++
			if pixel.R != 0xFF || pixel.G != 0xFF || pixel.B != 0xFF {
				t.Fatalf("pixel (%d,%d) = %v, want white", x, y, pixel)
			}
		}
	}
	if painted == 0 {
		t.Fatal("Mark(32) painted nothing")
	}
}

func TestMarkStrokeStaysLegibleAt16(t *testing.T) {
	img := Mark(16, Dark)

	var painted int
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if img.NRGBAAt(x, y).A >= 128 {
				painted++
			}
		}
	}
	// An unclamped 0.6 px stroke would leave nearly every pixel under half
	// coverage; the clamp must keep a readable ring.
	if painted < 20 {
		t.Fatalf("Mark(16) has %d solid pixels, want a legible glyph", painted)
	}
}

func TestAppIconPlateAndTransparentCorners(t *testing.T) {
	img := AppIcon(256)

	if corner := img.NRGBAAt(0, 0); corner.A != 0 {
		t.Fatalf("corner pixel = %v, want transparent outside the rounded plate", corner)
	}
	// The clock face is hollow: dead center shows the plate, not the mark.
	center := img.NRGBAAt(200, 128) // inside the ring, right of the hand
	if center != (color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF4, A: 0xFF}) {
		t.Fatalf("face pixel = %v, want plate fill", center)
	}
	// A point on the ring stroke: (512+340, 512) on the design grid.
	ring := img.NRGBAAt(213, 128)
	if ring.A != 0xFF || ring.R != Dark.R || ring.G != Dark.G || ring.B != Dark.B {
		t.Fatalf("ring pixel = %v, want dark mark", ring)
	}
}
