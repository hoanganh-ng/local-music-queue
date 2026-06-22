#!/usr/bin/env python3
"""Generate simple PNG icons for the Local Music Queue browser extension."""

from PIL import Image, ImageDraw
import os

SIZES = [16, 48, 128]
OUTPUT_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'icons')

def create_icon(size):
    """Create a simple music note icon at the given size."""
    img = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    
    # Colors
    bg_color = (62, 166, 255, 255)  # YouTube-like blue
    note_color = (255, 255, 255, 255)
    
    # Draw rounded rectangle background
    margin = int(size * 0.05)
    radius = int(size * 0.15)
    draw.rounded_rectangle(
        [margin, margin, size - margin - 1, size - margin - 1],
        radius=radius,
        fill=bg_color
    )
    
    # Draw a simple music note (eighth note)
    # Scale factors
    s = size / 128.0
    
    # Note head (ellipse)
    head_cx = int(48 * s)
    head_cy = int(88 * s)
    head_rx = int(18 * s)
    head_ry = int(14 * s)
    draw.ellipse(
        [head_cx - head_rx, head_cy - head_ry, head_cx + head_rx, head_cy + head_ry],
        fill=note_color
    )
    
    # Stem (vertical line)
    stem_x = int(66 * s)
    stem_top = int(24 * s)
    stem_bottom = int(88 * s)
    stem_width = max(2, int(6 * s))
    draw.rectangle(
        [stem_x - stem_width // 2, stem_top, stem_x + stem_width // 2, stem_bottom],
        fill=note_color
    )
    
    # Flag (curved line at top of stem)
    flag_points = [
        (stem_x, stem_top),
        (int(92 * s), int(36 * s)),
        (int(88 * s), int(48 * s)),
        (stem_x, int(36 * s))
    ]
    draw.polygon(flag_points, fill=note_color)
    
    return img


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)
    
    for size in SIZES:
        img = create_icon(size)
        output_path = os.path.join(OUTPUT_DIR, f'icon{size}.png')
        img.save(output_path, 'PNG')
        print(f'Created {output_path}')


if __name__ == '__main__':
    main()
