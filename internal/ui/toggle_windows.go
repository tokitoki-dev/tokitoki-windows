//go:build windows

package ui

import (
	"image"
	"image/color"
	"math"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// Windows has no switch control and walk exposes none, so the pill toggle
// from the product design is drawn here and shown in a clickable ImageView.
// The drawing uses the same supersampled coverage approach as the logo
// renderer, which keeps the rounded track and knob smooth at any DPI, and
// leaves the corners transparent so the dialog background shows through —
// walk blits bitmaps with AlphaBlend, and Go's color model hands it the
// premultiplied pixels that needs.

// Toggle metrics in 96dpi device-independent pixels.
const (
	toggleWidth  = 40
	toggleHeight = 22
	// toggleInset is the gap between the knob and the track edge, as a
	// fraction of the track height.
	toggleInset = 0.11
)

var (
	toggleOnColor       = color.NRGBA{R: 0x63, G: 0x66, B: 0xF1, A: 0xFF}
	toggleOffColorLight = color.NRGBA{R: 0xD1, G: 0xD5, B: 0xDB, A: 0xFF}
	toggleOffColorDark  = color.NRGBA{R: 0x52, G: 0x52, B: 0x5B, A: 0xFF}
	toggleKnobColor     = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

// toggle is a two-state pill switch. Both states are rendered up front so
// flipping is a bitmap swap rather than a redraw.
type toggle struct {
	view     *walk.ImageView
	on       bool
	onBmp    *walk.Bitmap
	offBmp   *walk.Bitmap
	onChange func(bool)
}

// newToggle renders both states at dpi. onChange runs on the UI thread after
// every flip; the caller reports failures, since the switch itself cannot.
func newToggle(dpi int, on bool, onChange func(bool)) (*toggle, error) {
	if dpi <= 0 {
		dpi = 96
	}
	width := toggleWidth * dpi / 96
	height := toggleHeight * dpi / 96
	light := appsUseLightTheme()

	onBmp, err := walk.NewBitmapFromImageForDPI(toggleImage(width, height, true, light), dpi)
	if err != nil {
		return nil, err
	}
	offBmp, err := walk.NewBitmapFromImageForDPI(toggleImage(width, height, false, light), dpi)
	if err != nil {
		onBmp.Dispose()
		return nil, err
	}
	return &toggle{on: on, onBmp: onBmp, offBmp: offBmp, onChange: onChange}, nil
}

// checked reports the current state.
func (t *toggle) checked() bool { return t.on }

// image returns the bitmap for the current state.
func (t *toggle) image() walk.Image {
	if t.on {
		return t.onBmp
	}
	return t.offBmp
}

// setChecked moves the switch without notifying, for reverting a rejected
// change.
func (t *toggle) setChecked(on bool) {
	t.on = on
	if t.view != nil {
		_ = t.view.SetImage(t.image())
	}
}

// flip toggles the switch and reports the new state.
func (t *toggle) flip() {
	t.setChecked(!t.on)
	if t.onChange != nil {
		t.onChange(t.on)
	}
}

func (t *toggle) dispose() {
	if t.onBmp != nil {
		t.onBmp.Dispose()
	}
	if t.offBmp != nil {
		t.offBmp.Dispose()
	}
}

// mutedTextColor is the secondary text color for row descriptions and the
// version line, in whichever theme the app is currently drawing.
func mutedTextColor() walk.Color {
	if appsUseLightTheme() {
		return walk.RGB(0x6B, 0x72, 0x80)
	}
	return walk.RGB(0xA1, 0xA1, 0xAA)
}

// separatorColor is the hairline that divides the dialog's sections.
func separatorColor() walk.Color {
	if appsUseLightTheme() {
		return walk.RGB(0xE5, 0xE7, 0xEB)
	}
	return walk.RGB(0x3F, 0x3F, 0x46)
}

// separatorLine is a one-pixel rule. walk's own HSeparator draws the etched
// system divider, which all but disappears against a dark dialog, so the line
// is a filled strip instead and stays visible in either theme.
func separatorLine() Widget {
	return Composite{
		Layout:     HBox{MarginsZero: true},
		Background: SolidColorBrush{Color: separatorColor()},
		MinSize:    Size{Height: 1},
		MaxSize:    Size{Height: 1},
	}
}

// settingRow lays out one preference the way the product design does: a bold
// title over a muted description on the left, the switch pinned right.
func settingRow(titleFont Font, muted walk.Color, t *toggle, title, description string) Widget {
	return Composite{
		Layout: HBox{MarginsZero: true, Spacing: 12},
		Children: []Widget{
			Composite{
				Layout: VBox{MarginsZero: true, Spacing: 2},
				Children: []Widget{
					Label{Text: title, Font: titleFont},
					Label{Text: description, TextColor: muted},
				},
			},
			HSpacer{},
			ImageView{
				AssignTo: &t.view,
				Image:    t.image(),
				Mode:     ImageViewModeCenter,
				MinSize:  Size{Width: toggleWidth, Height: toggleHeight},
				MaxSize:  Size{Width: toggleWidth, Height: toggleHeight},
				OnMouseDown: func(x, y int, button walk.MouseButton) {
					if button == walk.LeftButton {
						t.flip()
					}
				},
			},
		},
	}
}

// toggleImage draws the track and knob at width×height pixels on transparency.
func toggleImage(width, height int, on, light bool) *image.NRGBA {
	track := toggleOnColor
	if !on {
		track = toggleOffColorLight
		if !light {
			track = toggleOffColorDark
		}
	}

	w := float64(width)
	h := float64(height)
	inset := h * toggleInset
	knobRadius := h/2 - inset
	knobY := h / 2
	knobX := h / 2
	if on {
		knobX = w - h/2
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	const ss = 3 // supersamples per axis
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var trackCov, knobCov float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					trackCov += pixelCoverage(pillDistance(px, py, w, h))
					knobCov += pixelCoverage(math.Hypot(px-knobX, py-knobY) - knobRadius)
				}
			}
			trackCov /= ss * ss
			knobCov /= ss * ss
			img.SetNRGBA(x, y, overlay(toggleKnobColor, knobCov, track, trackCov))
		}
	}
	return img
}

// pillDistance is the signed distance to a rounded rectangle of the given
// size whose corner radius is half its height: negative inside.
func pillDistance(px, py, w, h float64) float64 {
	radius := h / 2
	qx := math.Abs(px-w/2) - (w/2 - radius)
	qy := math.Abs(py-h/2) - (h/2 - radius)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - radius
}

// pixelCoverage converts a signed distance to approximate pixel coverage with
// a one-pixel antialiasing ramp.
func pixelCoverage(d float64) float64 {
	return math.Max(0, math.Min(1, 0.5-d))
}

// overlay lays top over bottom and returns the resulting straight-alpha pixel.
func overlay(top color.NRGBA, topCov float64, bottom color.NRGBA, bottomCov float64) color.NRGBA {
	alpha := topCov + bottomCov*(1-topCov)
	if alpha == 0 {
		return color.NRGBA{}
	}
	blend := func(t, b uint8) uint8 {
		value := (float64(t)*topCov + float64(b)*bottomCov*(1-topCov)) / alpha
		return uint8(math.Round(math.Max(0, math.Min(255, value))))
	}
	return color.NRGBA{
		R: blend(top.R, bottom.R),
		G: blend(top.G, bottom.G),
		B: blend(top.B, bottom.B),
		A: uint8(math.Round(alpha * 255)),
	}
}
