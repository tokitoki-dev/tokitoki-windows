//! Procedural clock-mark renderer (mirrors Go `internal/logo`).
//!
//! The tray glyph and app icon are rendered at runtime from signed distance
//! fields — a ring plus one hand pointing to ten o'clock — with 3×3
//! supersampling and a 1 px anti-aliasing ramp. No bitmap assets.

// Geometry math converts freely between design units, device pixels, and
// channel bytes; the ranges are tiny (icon sizes), so lossy casts are fine.
#![expect(
    clippy::cast_possible_truncation,
    clippy::cast_precision_loss,
    clippy::cast_sign_loss
)]

/// Straight-alpha RGBA color.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Rgba {
    /// Red channel.
    pub r: u8,
    /// Green channel.
    pub g: u8,
    /// Blue channel.
    pub b: u8,
    /// Alpha channel (straight, not premultiplied).
    pub a: u8,
}

/// Dark mark color `#18181B` (light backgrounds).
pub const DARK: Rgba = Rgba {
    r: 0x18,
    g: 0x18,
    b: 0x1B,
    a: 0xFF,
};
/// White mark color (dark backgrounds / default taskbar).
pub const WHITE: Rgba = Rgba {
    r: 0xFF,
    g: 0xFF,
    b: 0xFF,
    a: 0xFF,
};
/// App-icon plate fill `#F5F5F4` (matches the macOS app icon).
const PLATE: Rgba = Rgba {
    r: 0xF5,
    g: 0xF5,
    b: 0xF4,
    a: 0xFF,
};

/// Design grid the geometry constants live on.
const DESIGN: f32 = 346.0;
const RING_RADIUS: f32 = 160.5;
const RING_STROKE: f32 = 25.0;
const HAND_STROKE: f32 = 25.0;
const RING_CENTER: (f32, f32) = (173.0, 173.0);
const HAND_TIP: (f32, f32) = (94.49, 113.64);
const HAND_BASE: (f32, f32) = (171.83, 183.79);
/// Minimum stroke half-widths in device pixels so the mark stays legible at
/// 16 px.
const MIN_RING_HALF: f32 = 0.8;
const MIN_HAND_HALF: f32 = 0.75;
/// Plate corner radius as a fraction of the side.
const PLATE_CORNER_FRACTION: f32 = 0.20;
/// The mark occupies this fraction of the app icon's side.
const APP_ICON_MARK_FRACTION: f32 = 0.72;

/// A square, straight-alpha RGBA image (row-major, 4 bytes per pixel).
pub struct Image {
    /// Side length in pixels.
    pub size: u32,
    /// `size * size * 4` bytes of RGBA data.
    pub rgba: Vec<u8>,
}

/// Renders the bare clock mark, full bleed on transparency.
#[must_use]
pub fn mark(size: u32, color: Rgba) -> Image {
    let mark = MarkField::new(size as f32, 0.0);
    render(size, |x, y| {
        let coverage = coverage_at(x, y, |px, py| mark.distance(px, py));
        (color, coverage)
    })
}

/// Renders the dark mark on a light rounded-rect plate (the app icon).
#[must_use]
pub fn app_icon(size: u32) -> Image {
    let edge = size as f32;
    let glyph_edge = edge * APP_ICON_MARK_FRACTION;
    let mark = MarkField::new(glyph_edge, (edge - glyph_edge) / 2.0);
    let corner = edge * PLATE_CORNER_FRACTION;
    let half = edge / 2.0;

    render(size, |x, y| {
        let plate = coverage_at(x, y, |px, py| {
            rounded_rect_distance(px - half, py - half, half, corner)
        });
        let glyph = coverage_at(x, y, |px, py| mark.distance(px, py));
        blend(DARK, glyph, PLATE, plate)
    })
}

/// The mark's distance field, scaled to `side` device pixels and offset.
struct MarkField {
    center: (f32, f32),
    radius: f32,
    ring_half: f32,
    hand_a: (f32, f32),
    hand_b: (f32, f32),
    hand_half: f32,
}

impl MarkField {
    fn new(side: f32, offset: f32) -> Self {
        let scale = side / DESIGN;
        let at = |p: (f32, f32)| (p.0 * scale + offset, p.1 * scale + offset);
        Self {
            center: at(RING_CENTER),
            radius: RING_RADIUS * scale,
            ring_half: (RING_STROKE / 2.0 * scale).max(MIN_RING_HALF),
            hand_a: at(HAND_TIP),
            hand_b: at(HAND_BASE),
            hand_half: (HAND_STROKE / 2.0 * scale).max(MIN_HAND_HALF),
        }
    }

    /// Signed distance in device pixels; negative means inside the mark.
    fn distance(&self, x: f32, y: f32) -> f32 {
        let ring =
            (length(x - self.center.0, y - self.center.1) - self.radius).abs() - self.ring_half;
        let hand = segment_distance(x, y, self.hand_a, self.hand_b) - self.hand_half;
        ring.min(hand)
    }
}

fn render(size: u32, shade: impl Fn(u32, u32) -> (Rgba, f32)) -> Image {
    let mut rgba = Vec::with_capacity((size * size * 4) as usize);
    for y in 0..size {
        for x in 0..size {
            let (color, coverage) = shade(x, y);
            let alpha = (coverage * f32::from(color.a)).round().clamp(0.0, 255.0) as u8;
            rgba.extend_from_slice(&[color.r, color.g, color.b, alpha]);
        }
    }
    Image { size, rgba }
}

/// 3×3 supersampled coverage of a distance field with a 1 px AA ramp.
fn coverage_at(x: u32, y: u32, distance: impl Fn(f32, f32) -> f32) -> f32 {
    let mut total = 0.0;
    for sub_y in 0..3 {
        for sub_x in 0..3 {
            let px = x as f32 + (sub_x as f32 + 0.5) / 3.0;
            let py = y as f32 + (sub_y as f32 + 0.5) / 3.0;
            total += (0.5 - distance(px, py)).clamp(0.0, 1.0);
        }
    }
    total / 9.0
}

/// Straight-alpha "glyph over plate" composite for one pixel.
fn blend(top: Rgba, top_cov: f32, bottom: Rgba, bottom_cov: f32) -> (Rgba, f32) {
    let out_cov = top_cov + bottom_cov * (1.0 - top_cov);
    if out_cov <= f32::EPSILON {
        return (bottom, 0.0);
    }
    let channel = |t: u8, b: u8| {
        let value =
            (f32::from(t) * top_cov + f32::from(b) * bottom_cov * (1.0 - top_cov)) / out_cov;
        value.round().clamp(0.0, 255.0) as u8
    };
    (
        Rgba {
            r: channel(top.r, bottom.r),
            g: channel(top.g, bottom.g),
            b: channel(top.b, bottom.b),
            a: 0xFF,
        },
        out_cov,
    )
}

fn length(x: f32, y: f32) -> f32 {
    x.hypot(y)
}

/// Distance from `(px, py)` to the segment `start`–`end`.
fn segment_distance(px: f32, py: f32, start: (f32, f32), end: (f32, f32)) -> f32 {
    let (abx, aby) = (end.0 - start.0, end.1 - start.1);
    let (apx, apy) = (px - start.0, py - start.1);
    let ab_len_sq = abx * abx + aby * aby;
    let t = if ab_len_sq <= f32::EPSILON {
        0.0
    } else {
        ((apx * abx + apy * aby) / ab_len_sq).clamp(0.0, 1.0)
    };
    length(apx - t * abx, apy - t * aby)
}

/// Signed distance to a rounded rectangle centered at the origin.
fn rounded_rect_distance(x: f32, y: f32, half_extent: f32, radius: f32) -> f32 {
    let qx = x.abs() - (half_extent - radius);
    let qy = y.abs() - (half_extent - radius);
    length(qx.max(0.0), qy.max(0.0)) + qx.max(qy).min(0.0) - radius
}

#[cfg(test)]
mod tests {
    use super::*;

    fn alpha_at(image: &Image, x: u32, y: u32) -> u8 {
        image.rgba[((y * image.size + x) * 4 + 3) as usize]
    }

    mod mark {
        use super::*;

        #[test]
        fn corner_pixel_is_transparent() {
            let image = mark(16, WHITE);
            assert_eq!(alpha_at(&image, 0, 0), 0);
        }

        #[test]
        fn center_pixel_is_partially_covered_by_the_hand() {
            let image = mark(16, WHITE);
            assert!(
                alpha_at(&image, 8, 8) > 100,
                "got {}",
                alpha_at(&image, 8, 8)
            );
        }

        #[test]
        fn stays_visible_at_sixteen_px() {
            let image = mark(16, WHITE);
            let opaque = image.rgba.chunks_exact(4).filter(|px| px[3] > 128).count();
            assert!(opaque > 20, "only {opaque} opaque pixels");
        }
    }

    mod app_icon {
        use super::*;

        #[test]
        fn corner_pixel_is_outside_the_rounded_plate() {
            let image = app_icon(64);
            assert_eq!(alpha_at(&image, 0, 0), 0);
        }

        #[test]
        fn hand_midpoint_pixel_is_the_dark_mark() {
            let image = app_icon(64);
            let scale = 64.0 * APP_ICON_MARK_FRACTION / DESIGN;
            let offset = 64.0 * (1.0 - APP_ICON_MARK_FRACTION) / 2.0;
            let x = (f32::midpoint(HAND_TIP.0, HAND_BASE.0) * scale + offset) as u32;
            let y = (f32::midpoint(HAND_TIP.1, HAND_BASE.1) * scale + offset) as u32;
            let idx = ((y * 64 + x) * 4) as usize;
            assert_eq!(image.rgba[idx], DARK.r);
        }

        #[test]
        fn edge_midpoint_is_the_plate_fill() {
            let image = app_icon(64);
            let idx = ((32 * 64 + 2) * 4) as usize;
            assert_eq!(image.rgba[idx], PLATE.r);
        }
    }
}
