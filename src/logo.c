#include "logo.h"

#include <math.h>

/* Geometry on the 346-unit design grid. */
#define DESIGN 346.0f
#define RING_RADIUS 160.5f
#define RING_STROKE 25.0f
#define HAND_STROKE 25.0f
#define CENTER_X 173.0f
#define CENTER_Y 173.0f
#define HAND_TIP_X 94.49f
#define HAND_TIP_Y 113.64f
#define HAND_BASE_X 171.83f
#define HAND_BASE_Y 183.79f
/* Stroke half-width floors in device px keep the mark legible at 16 px. */
#define MIN_RING_HALF 0.8f
#define MIN_HAND_HALF 0.75f

typedef struct Field {
    float cx, cy, radius, ring_half;
    float ax, ay, bx, by, hand_half;
} Field;

static float length2(float x, float y) {
    return sqrtf(x * x + y * y);
}

static float segment_distance(float px, float py, const Field *f) {
    float abx = f->bx - f->ax;
    float aby = f->by - f->ay;
    float apx = px - f->ax;
    float apy = py - f->ay;
    float ab_len_sq = abx * abx + aby * aby;
    float t = 0.0f;
    if (ab_len_sq > 1e-6f) {
        t = (apx * abx + apy * aby) / ab_len_sq;
        t = t < 0.0f ? 0.0f : (t > 1.0f ? 1.0f : t);
    }
    return length2(apx - t * abx, apy - t * aby);
}

/* Signed distance in device px; negative means inside the mark. */
static float mark_distance(float px, float py, const Field *f) {
    float ring = fabsf(length2(px - f->cx, py - f->cy) - f->radius) - f->ring_half;
    float hand = segment_distance(px, py, f) - f->hand_half;
    return ring < hand ? ring : hand;
}

void logo_mark(uint32_t size, bool white, uint8_t *rgba) {
    float scale = (float)size / DESIGN;
    Field f;
    f.cx = CENTER_X * scale;
    f.cy = CENTER_Y * scale;
    f.radius = RING_RADIUS * scale;
    f.ring_half = RING_STROKE / 2.0f * scale;
    if (f.ring_half < MIN_RING_HALF) {
        f.ring_half = MIN_RING_HALF;
    }
    f.ax = HAND_TIP_X * scale;
    f.ay = HAND_TIP_Y * scale;
    f.bx = HAND_BASE_X * scale;
    f.by = HAND_BASE_Y * scale;
    f.hand_half = HAND_STROKE / 2.0f * scale;
    if (f.hand_half < MIN_HAND_HALF) {
        f.hand_half = MIN_HAND_HALF;
    }

    uint8_t r = white ? 0xFF : 0x18;
    uint8_t g = white ? 0xFF : 0x18;
    uint8_t b = white ? 0xFF : 0x1B;

    for (uint32_t y = 0; y < size; y++) {
        for (uint32_t x = 0; x < size; x++) {
            /* 3x3 supersampled coverage with a 1 px AA ramp. */
            float total = 0.0f;
            for (int sy = 0; sy < 3; sy++) {
                for (int sx = 0; sx < 3; sx++) {
                    float px = (float)x + ((float)sx + 0.5f) / 3.0f;
                    float py = (float)y + ((float)sy + 0.5f) / 3.0f;
                    float coverage = 0.5f - mark_distance(px, py, &f);
                    if (coverage < 0.0f) {
                        coverage = 0.0f;
                    } else if (coverage > 1.0f) {
                        coverage = 1.0f;
                    }
                    total += coverage;
                }
            }
            uint8_t alpha = (uint8_t)(total / 9.0f * 255.0f + 0.5f);
            uint8_t *px_out = rgba + ((size_t)y * size + x) * 4;
            px_out[0] = r;
            px_out[1] = g;
            px_out[2] = b;
            px_out[3] = alpha;
        }
    }
}
