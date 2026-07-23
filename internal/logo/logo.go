// Package logo renders the Tokitoki mark — a clock face with one hand — at
// any pixel size. One geometric description serves every consumer: the tray
// icon picks a theme color at runtime and the icon generator bakes the app
// icon, so there are no bitmap assets to drift out of sync.
package logo

import (
	"image"
	"image/color"
	"math"
)

// Geometry of the mark on its 1024-unit design grid, taken from the product
// SVG: a circle of radius 340 stroked 40 wide, and a hand running from ten
// o'clock to the center.
const (
	design     = 1024.0
	ringRadius = 340.0
	ringStroke = 40.0
	handStroke = 36.0
)

var (
	ringCenter = point{512, 512}
	handTip    = point{344, 386}
	handBase   = point{504, 510}
)

// Dark is the product mark color for light backgrounds.
var Dark = color.NRGBA{R: 0x18, G: 0x18, B: 0x1B, A: 0xFF}

// White is the product mark color for dark backgrounds.
var White = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

// plateColor is the light app-icon background, matching the macOS app icon
// fill so the brand reads the same on both platforms.
var plateColor = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF4, A: 0xFF}

// plateCornerRadius is the app icon plate's corner radius as a fraction of
// the icon side.
const plateCornerRadius = 0.20

type point struct{ x, y float64 }

// Mark renders the bare mark in c at size×size pixels, on transparency, for
// the tray. Below roughly 40 px the true stroke width fades toward nothing,
// so strokes are clamped to stay legible at small sizes.
func Mark(size int, c color.NRGBA) *image.NRGBA {
	return render(size, false, c)
}

// AppIcon renders the application icon at size×size pixels: the dark mark on
// a light rounded-rectangle plate, full bleed.
func AppIcon(size int) *image.NRGBA {
	return render(size, true, Dark)
}

func render(size int, withPlate bool, markColor color.NRGBA) *image.NRGBA {
	scale := float64(size) / design
	halfRing := math.Max(ringStroke/2*scale, 0.8)
	halfHand := math.Max(handStroke/2*scale, 0.75)
	corner := plateCornerRadius * float64(size)

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 3 // supersamples per axis
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var markCov, plateCov float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					markCov += coverage(markDistance(px, py, scale, halfRing, halfHand))
					if withPlate {
						plateCov += coverage(roundRectDistance(px, py, float64(size), corner))
					}
				}
			}
			markCov /= ss * ss
			plateCov /= ss * ss
			img.SetNRGBA(x, y, composite(markColor, markCov, plateCov))
		}
	}
	return img
}

// markDistance is the signed distance from (px,py) to the mark's edge:
// negative inside the ring or the hand.
func markDistance(px, py, scale, halfRing, halfHand float64) float64 {
	dx := px - ringCenter.x*scale
	dy := py - ringCenter.y*scale
	ring := math.Abs(math.Hypot(dx, dy)-ringRadius*scale) - halfRing

	hand := segmentDistance(
		point{px, py},
		point{handTip.x * scale, handTip.y * scale},
		point{handBase.x * scale, handBase.y * scale},
	) - halfHand

	return math.Min(ring, hand)
}

func segmentDistance(p, a, b point) float64 {
	abx, aby := b.x-a.x, b.y-a.y
	apx, apy := p.x-a.x, p.y-a.y
	t := (apx*abx + apy*aby) / (abx*abx + aby*aby)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(apx-t*abx, apy-t*aby)
}

// roundRectDistance is the signed distance to a full-bleed rounded rectangle
// of the given side and corner radius.
func roundRectDistance(px, py, side, radius float64) float64 {
	half := side / 2
	qx := math.Abs(px-half) - (half - radius)
	qy := math.Abs(py-half) - (half - radius)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - radius
}

// coverage converts a signed distance to approximate pixel coverage with a
// one-pixel antialiasing ramp.
func coverage(d float64) float64 {
	return math.Max(0, math.Min(1, 0.5-d))
}

// composite lays the mark over the plate and returns the resulting
// straight-alpha pixel.
func composite(mark color.NRGBA, markCov, plateCov float64) color.NRGBA {
	alpha := markCov + plateCov*(1-markCov)
	if alpha == 0 {
		return color.NRGBA{}
	}
	blend := func(markC, plateC uint8) uint8 {
		value := (float64(markC)*markCov + float64(plateC)*plateCov*(1-markCov)) / alpha
		return uint8(math.Round(value))
	}
	return color.NRGBA{
		R: blend(mark.R, plateColor.R),
		G: blend(mark.G, plateColor.G),
		B: blend(mark.B, plateColor.B),
		A: uint8(math.Round(alpha * 255)),
	}
}
