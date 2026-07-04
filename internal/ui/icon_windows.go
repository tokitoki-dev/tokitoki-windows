//go:build windows

package ui

import (
	"image"
	"image/color"
	"math"

	"github.com/lxn/walk"
)

const iconSize = 128

func newAppIcon() (*walk.Icon, error) {
	return walk.NewIconFromImage(drawAppIcon(iconSize))
}

func drawAppIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2
	black := color.RGBA{R: 20, G: 22, B: 25, A: 255}
	orange := color.RGBA{R: 253, G: 98, B: 9, A: 255}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx := float64(x) + 0.5 - center
			fy := float64(y) + 0.5 - center
			radius := math.Hypot(fx, fy)
			angle := math.Atan2(fy, fx)

			switch {
			case inStroke(radius, center*0.72, 9) && !angleBetween(angle, -0.35, 0.8):
				img.SetRGBA(x, y, black)
			case inStroke(radius, center*0.42, 8) && !angleBetween(angle, -0.4, 0.85):
				img.SetRGBA(x, y, black)
			case inStroke(radius, center*0.72, 10) && angleBetween(angle, -0.55, 0.75):
				img.SetRGBA(x, y, orange)
			case inStroke(radius, center*0.42, 9) && angleBetween(angle, -0.55, 0.65):
				img.SetRGBA(x, y, orange)
			case math.Abs(fx) < center*0.07 && fy > -center*0.38 && fy < center*0.22:
				img.SetRGBA(x, y, black)
			case math.Abs(fy+center*0.34) < center*0.06 && math.Abs(fx) < center*0.28:
				img.SetRGBA(x, y, black)
			}
		}
	}
	return img
}

func inStroke(radius, target, width float64) bool {
	return math.Abs(radius-target) <= width
}

func angleBetween(angle, start, end float64) bool {
	if start <= end {
		return angle >= start && angle <= end
	}
	return angle >= start || angle <= end
}
