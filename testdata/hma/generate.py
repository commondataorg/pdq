# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "threatexchange==1.2.16",
#     "pdqhash==0.2.8",
#     "pillow==12.3.0",
#     "numpy==2.5.3",
# ]
# ///
"""Regenerate the HMA cross-validation fixture.

Writes one small image per format and mode into this directory and records the
hash and quality python-threatexchange computes for each in hashes.json.

    uv run testdata/hma/generate.py
"""

import importlib.metadata
import io
import json
import struct
from pathlib import Path

import numpy as np
from PIL import Image
from threatexchange.signal_type.pdq.pdq_hasher import pdq_from_file

HERE = Path(__file__).parent

# Decoded with a different JPEG or WebP decoder than Pillow's, so these can
# land a few bits off rather than exact.
LOSSY = {
    "jpg-420",
    "jpg-422",
    "jpg-444",
    "jpg-progressive",
    "jpg-grey",
    "jpg-cmyk",
    "webp-lossy",
    "webp-lossy-alpha",
}


def sources() -> dict[str, Image.Image]:
    photo = Image.open(HERE.parent / "bridge.jpg").convert("RGB")
    photo = photo.resize(
        (192, photo.height * 192 // photo.width), Image.Resampling.LANCZOS
    )
    rng = np.random.default_rng(7)
    y, x = np.mgrid[0:120, 0:160]
    synth = np.stack(
        [
            (x * 255 // 159),
            (y * 255 // 119),
            ((np.sin(x / 7.0) * np.cos(y / 5.0) + 1) * 127.5).astype(int),
        ],
        axis=2,
    )
    synth = np.clip(synth + rng.integers(-20, 21, synth.shape), 0, 255).astype(np.uint8)
    return {"photo": photo, "synth": Image.fromarray(synth, "RGB")}


def patched_gif(
    frame: Image.Image,
    screen: tuple[int, int],
    at: tuple[int, int] = (0, 0),
    transparent: int | None = None,
) -> bytes:
    """A one-frame GIF with its logical screen, the frame's position and,
    optionally, the transparent index in its graphic control extension
    rewritten."""
    buf = io.BytesIO()
    if transparent is None:
        frame.save(buf, "gif")
    else:
        # Unoptimized, so an unused index 0 keeps its GCE.
        frame.save(buf, "gif", transparency=0, optimize=False)
    data = bytearray(buf.getvalue())
    struct.pack_into("<HH", data, 6, *screen)
    flags = data[10]
    pos = 13 + (3 * 2 ** ((flags & 7) + 1) if flags & 0x80 else 0)
    patched = False
    while data[pos] == 0x21:  # extension blocks
        if data[pos + 1] == 0xF9 and transparent is not None:
            data[pos + 6] = transparent
            patched = True
        pos += 2
        while data[pos]:
            pos += data[pos] + 1
        pos += 1
    if data[pos] != 0x2C:
        raise ValueError("image descriptor not found")
    if transparent is not None and not patched:
        raise ValueError("no graphic control extension to set transparency in")
    struct.pack_into("<HH", data, pos + 1, *at)
    return bytes(data)


def associated_alpha_tiff(rgba: Image.Image) -> bytes:
    """An uncompressed RGBA TIFF with premultiplied samples (ExtraSamples=1)."""
    w, h = rgba.size
    px = np.asarray(rgba).astype(np.uint32)
    px[..., :3] = px[..., :3] * px[..., 3:4] // 255
    pixels = px.astype(np.uint8).tobytes()
    tags = [  # tag, type, count, value; type 3 = SHORT, 4 = LONG
        (256, 4, 1, w),
        (257, 4, 1, h),
        (258, 3, 4, None),  # BitsPerSample, stored after the IFD
        (259, 3, 1, 1),
        (262, 3, 1, 2),
        (273, 4, 1, None),  # StripOffsets, the pixel data
        (277, 3, 1, 4),
        (278, 4, 1, h),
        (279, 4, 1, len(pixels)),
        (284, 3, 1, 1),
        (338, 3, 1, 1),  # ExtraSamples: associated alpha
    ]
    ifd_size = 2 + 12 * len(tags) + 4
    bits_at = 8 + ifd_size
    pixels_at = bits_at + 8
    out = bytearray(b"II*\x00" + struct.pack("<I", 8) + struct.pack("<H", len(tags)))
    for tag, typ, count, value in tags:
        if tag == 258:
            value = bits_at
        elif tag == 273:
            value = pixels_at
        if typ == 3 and count == 1:
            out += struct.pack("<HHIHH", tag, typ, count, value, 0)
        else:
            out += struct.pack("<HHII", tag, typ, count, value)
    out += struct.pack("<I", 0)
    out += struct.pack("<4H", 8, 8, 8, 8)
    out += pixels
    return bytes(out)


def variants(rgb: Image.Image) -> dict[str, tuple[Image.Image | bytes, str, dict]]:
    w, h = rgb.size
    alpha = Image.fromarray(
        np.tile(np.linspace(0, 255, w, dtype=np.uint8), (h, 1)), "L"
    )
    rgba = rgb.copy()
    rgba.putalpha(alpha)
    grey_alpha = rgb.convert("L")
    grey_alpha.putalpha(alpha)
    palette = rgb.convert("P", palette=Image.Palette.ADAPTIVE, colors=256)
    grey16 = Image.fromarray(np.asarray(rgb.convert("L")).astype(np.uint16) * 257)
    small = palette.crop((0, 0, w * 3 // 4, h * 3 // 4))
    small16 = rgb.convert("P", palette=Image.Palette.ADAPTIVE, colors=16).crop(
        (0, 0, w * 3 // 4, h * 3 // 4)
    )
    bilevel = rgb.convert("1")
    return {
        "jpg-420": (rgb, "jpg", {"quality": 90, "subsampling": 2}),
        "jpg-422": (rgb, "jpg", {"quality": 90, "subsampling": 1}),
        "jpg-444": (rgb, "jpg", {"quality": 90, "subsampling": 0}),
        "jpg-progressive": (rgb, "jpg", {"quality": 90, "progressive": True}),
        "jpg-grey": (rgb.convert("L"), "jpg", {"quality": 90}),
        "jpg-cmyk": (rgb.convert("CMYK"), "jpg", {"quality": 90}),
        "png-rgb": (rgb, "png", {}),
        "png-rgba": (rgba, "png", {}),
        "png-grey": (rgb.convert("L"), "png", {}),
        "png-grey-alpha": (grey_alpha, "png", {}),
        "png-palette": (palette, "png", {}),
        "png-palette-16": (
            rgb.convert("P", palette=Image.Palette.ADAPTIVE, colors=16),
            "png",
            {"bits": 4},
        ),
        "png-palette-trns": (palette, "png", {"transparency": 0}),
        "png-grey16": (grey16, "png", {}),
        "png-grey16-trns": (grey16, "png", {"transparency": 0}),
        "png-1bit": (bilevel, "png", {}),
        "png-1bit-trns": (bilevel, "png", {"transparency": 0}),
        "gif": (palette, "gif", {}),
        "gif-offset-frame": (
            patched_gif(small, (w, h), (w // 8, h // 8)),
            "gif",
            {},
        ),
        "gif-offset-transparent": (
            patched_gif(small, (w, h), (w // 8, h // 8), transparent=37),
            "gif",
            {},
        ),
        # A 16-colour palette with transparent index 200, past its end.
        "gif-transparent-past-palette": (
            patched_gif(small16, (w, h), (w // 8, h // 8), transparent=200),
            "gif",
            {},
        ),
        # A full-size frame on a screen declared half as large.
        "gif-frame-overruns-screen": (
            patched_gif(palette, (w // 2, h // 2)),
            "gif",
            {},
        ),
        "webp-lossless": (rgb, "webp", {"lossless": True}),
        "webp-alpha": (rgba, "webp", {"lossless": True}),
        "webp-lossy": (rgb, "webp", {"quality": 90}),
        "webp-lossy-alpha": (rgba, "webp", {"quality": 90}),
        "bmp": (rgb, "bmp", {}),
        "tiff-rgb": (rgb, "tiff", {}),
        "tiff-rgba": (rgba, "tiff", {}),
        "tiff-rgba-associated": (associated_alpha_tiff(rgba), "tiff", {}),
        "tiff-palette": (palette, "tiff", {}),
        "tiff-1bit": (bilevel, "tiff", {}),
        "tiff-1bit-g4": (bilevel, "tiff", {"compression": "group4"}),
    }


def main() -> None:
    cases = []
    for source, rgb in sources().items():
        for kind, (img, ext, kw) in variants(rgb).items():
            name = f"{source}-{kind}.{ext}"
            if isinstance(img, bytes):
                (HERE / name).write_bytes(img)
            else:
                img.save(HERE / name, **kw)
            pdq_hex, quality = pdq_from_file(HERE / name)
            cases.append(
                {
                    "file": name,
                    "mode": Image.open(HERE / name).mode,
                    "hash_hex": pdq_hex,
                    "quality": quality,
                    "lossy": kind in LOSSY,
                }
            )
    versions = {
        p: importlib.metadata.version(p)
        for p in ("threatexchange", "pdqhash", "pillow", "numpy")
    }
    (HERE / "hashes.json").write_text(
        json.dumps({"generated_with": versions, "cases": cases}, indent=1) + "\n"
    )


if __name__ == "__main__":
    main()
