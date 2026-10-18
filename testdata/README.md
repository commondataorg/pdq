# testdata provenance

## bridge.jpg

Meta's bridge photograph, `pdq/data/bridge-mods/aaa-orig.jpg` from
facebook/ThreatExchange, licensed BSD-3-Clause (see
LICENSE.ThreatExchange.txt).

- Source: https://raw.githubusercontent.com/facebook/ThreatExchange/main/pdq/data/bridge-mods/aaa-orig.jpg
- sha256: b5b0799616df52d475a3968dc7e54f1d0724c912244ffa6175bc786375dd7298
- 361182 bytes, 1600x1004 JPEG

Upstream ships it (plus blur, sharpen, shrink and square-resize variants in
the same directory) as PDQ regression-test input.

## reference_hashes.json

24 synthetic cases (solid colors, gradients, checkerboards, noise, stripes,
concentric patterns), each an RGB byte array with the hash and quality that
pdqhash (Meta's PDQ C++ via Python bindings) computes for it. The cases are
generated, not derived from any image file, so replacing bridge.jpg never
requires regenerating this file.

## hma/

66 fixtures covering 33 formats and modes on two sources (a photo resized
from bridge.jpg, and a synthetic pattern), with the hash and quality
python-threatexchange computes for each recorded in hashes.json.
Regenerate with:

    uv run testdata/hma/generate.py

The script pins threatexchange 1.2.16, pdqhash 0.2.8, pillow 12.3.0 and
numpy 2.5.3.
