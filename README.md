# pdq

Pure-Go [PDQ](https://github.com/facebook/ThreatExchange/tree/main/pdq) and
[vPDQ](https://github.com/facebook/ThreatExchange/tree/main/vpdq): 256-bit
perceptual hashing of images and videos, reproducing Meta's reference
implementations.

The root package, `pdq`, matches
[pdqhash-python](https://pypi.org/project/pdqhash/) — the PDQ library
python-threatexchange (HMA) hashes with — bit for bit wherever the decoded
pixel bytes agree; JPEG and lossy WebP land a few bits off, because Go's
decoders round a few pixel levels differently from libjpeg-turbo and libwebp.
`vpdq` matches the ThreatExchange C++ vPDQ frame for frame given x86-64
ffmpeg. No CGo anywhere: the module is pure Go, plus an ffmpeg subprocess for
vpdq.

## Install

    go get github.com/commondataorg/pdq

Package `pdq` is the module root; video hashing is
`github.com/commondataorg/pdq/vpdq`. Import-only, no main packages. Go 1.27+;
the only third-party dependency is golang.org/x/image. vpdq additionally
requires `ffmpeg` and `ffprobe` on PATH.

## pdq — image hashing

Entry points: `ComputeHash`, `ComputeHashFromFile`, `ComputeHashFromBytes`,
`ComputeHashFromRGBBytes` (for callers that already decoded elsewhere, e.g.
ffmpeg frames), `ComputeHashBase64`.

Pipeline: decode → luminance → two Jarosz tent-filter downscale passes →
64×64 → DCT to 16×16 → median threshold → 256 bits → hex.

### Luma: pdqhash-python's arithmetic, not the C++'s

Luma is computed in float64 and truncated to float32, as pdqhash-python does.
The ThreatExchange C++ computes it in float32, so the image entry points are
not bit-identical to the C++ and differ by a bit or two on some images.
`ComputeHashFromRGB24` takes its own `RGB24Option`s — an image option cannot
be passed to it — and `CPPLuma()` switches it to the C++ float32 luma, which
vpdq uses, since vPDQ is defined on the C++.

### Rendered colour vs HMACompatible

There are two ways to turn an image's pixels into luma:

- **Default: rendered colour.** Palette images are hashed by their colours,
  CMYK is converted to RGB, 16-bit channels use the high byte, and alpha is
  premultiplied. Re-encoding an image without changing how it looks (a
  permuted palette, a CMYK or 16-bit copy) leaves the hash where it was, so
  use this for enforcement matching.
- **`HMACompatible()`:** the values HMA
  ([python-threatexchange](https://github.com/facebook/ThreatExchange/tree/main/python-threatexchange))
  hashes, so the result string-matches corpora HMA built. HMA hashes Pillow's
  native-mode array, so:

  - alpha is ignored (premultiplied images are first unpremultiplied);
  - palette images are hashed by their indices;
  - CMYK channels are read as RGB;
  - 16-bit grey is clipped at 255.

  Those samples are the uploader's to choose, so the same re-encodings move
  this hash far from the original; do not use it for enforcement.

For RGB JPEG, greyscale and opaque RGB images the two agree.

### File-level decoding

Some of what HMA hashes is only visible in the file, so `ComputeHashFromFile`
and `ComputeHashFromBytes` apply it with `HMACompatible()`, and
`ComputeHash(image.Image)` cannot:

- only a GIF's first frame is decoded, placed on its logical screen (grown to
  fit a frame that overruns it) and filled outside the frame with the frame's
  transparent index, or 0, even when that index is past the palette's end;
- a 16-bit greyscale PNG with a tRNS chunk stays 16-bit grey;
- a 1-bit PNG or TIFF holds 0 and 1, which changes the quality score though
  not the hash;
- a CCITT Group 3 or 4 TIFF marked BlackIsZero is read the way libtiff reads
  it: golang.org/x/image/tiff decodes it inverted.

They also decode BMP and TIFF, which the default path does not. An image over
Pillow's decompression-bomb limit (178,956,970 pixels, counting a GIF's grown
screen), which HMA refuses to hash, is rejected before its pixels are
allocated, and `ComputeHashFromFile` streams the file rather than reading it
whole. Some malformed files fail on one side only:

- CMYK TIFF, and a GIF graphic control extension that is not exactly 4 bytes
  or carries an extra sub-block, fail to decode here where HMA hashes them;
- a TIFF with BitsPerSample stored as a BYTE hashes here where Pillow refuses
  it.

`testdata/hma` pins `HMACompatible()` against python-threatexchange across 33
formats and modes on two sources, including the file-level cases above;
`testdata/hma/generate.py` regenerates it with pinned versions. Lossless
formats match hash and quality exactly. JPEG and lossy WebP can land a few
bits off, because Go's decoders round a few pixel levels differently from
libjpeg-turbo and libwebp.

The Jarosz box filters avoid their two bottlenecks in every build. The column
pass sweeps whole rows and keeps one running sum per column, so it reads
memory sequentially instead of striding. The row pass advances four rows at
once, so their running-sum chains overlap.

### SIMD

Built with `GOEXPERIMENT=simd`, the DCT and the column pass also vectorize
through Go's portable [`simd`](https://go.dev/blog/simd-experiment) package
(AVX2/AVX-512 on amd64, NEON on arm64). The output stays bit-identical, since
every lane runs the same unfused multiply, add and divide as the scalar code.
When `simd` would only emulate vectors, the scalar code runs. A consumer opts
in by setting `GOEXPERIMENT=simd` in its own build. The test suite runs the
same fixtures through both paths (`GOEXPERIMENT=simd go test ./...`, and
`GOEXPERIMENT=simd GODEBUG=simd=128 go test .` for the lane width arm64
gets), so any drift between them fails CI.

Contributors: the vector code must stay bit-identical to the scalar code. Use
`Mul` then `Add`, never `MulAdd` — fused multiply-add rounds differently in
the last bit — and keep each lane's accumulation order the same as the scalar
loop's. The row filter has no vector version: vectorizing across rows needs a
transpose, and the portable `simd` package has no shuffles, so a scalar
transpose costs more than it saves. Go 1.27's simd rewriter panics on a
`for range n` with no loop variable inside a function that uses simd types
(golang/go#81264), which is why the phase loops in `simd.go` count
explicitly.

Median hash time on an Intel Core Ultra 7 155H, pinned to one P-core, Go
1.27.1:

| Input                       | Default build | `GOEXPERIMENT=simd` |
| --------------------------- | ------------- | ------------------- |
| 1600x1004 JPEG, with decode | 27.3 ms       | 25.7 ms             |
| 1024x1024 synthetic luma    | 3.8 ms        | 2.6 ms              |
| 64x64 luma, core hash       | 157 us        | 24.8 us             |

### Compared with pdqhash-python

Median time to hash `testdata/bridge.jpg` (1600x1004 JPEG), on the same
machine and P-core as above:

| Work                                | pdqhash-python | Go, default | Go, `GOEXPERIMENT=simd` |
| ----------------------------------- | -------------- | ----------- | ----------------------- |
| File to hash, HMA path              | 25.5 ms        | 27.3 ms     | 25.6 ms                 |
| Decoded RGB pixels to hash          | 19.2 ms        | 6.5 ms      | 5.0 ms                  |

- **File to hash** is python-threatexchange's `pdq_from_file` (Pillow decode,
  then `pdqhash.compute`) against `ComputeHashFromFile(path,
  HMACompatible())`. `image/jpeg` takes about 14 ms on this file where
  Pillow (libjpeg-turbo 3.1.3) decodes it to an array in about 6 ms, and the
  Go path then reads luma from the decoded `image.Image` pixel by pixel. For
  pixels already decoded elsewhere, `ComputeHashFromRGB24` skips both.
- **Decoded RGB pixels to hash** is `pdqhash.compute` on Pillow's `uint8`
  1004x1600x3 array against `ComputeHashFromRGB24` on the same image's
  interleaved RGB bytes. Both use pdqhash-python's luma and return the same
  hash given identical bytes, so this row is the hashing alone.

pdqhash 0.2.8 publishes no Linux wheels, so on Linux it is compiled from the
sdist: here with GCC 16.2.1 at `-O3`, no `-march`, against CPython 3.14.8.
Python figures are the middle of three runs of the script, each the median
of 200 calls after 10 warm-up calls; Go figures are the benchstat median of
10 runs at `-cpu 1`. To reproduce:

    taskset -c 2 go test -run '^$' -bench 'FileHMA$|RGB24$' -cpu 1 -count 10 .
    GOEXPERIMENT=simd taskset -c 2 go test -run '^$' -bench 'FileHMA$|RGB24$' -cpu 1 -count 10 .
    taskset -c 2 uv run scripts/bench_pdqhash.py

Pick a performance core for `taskset` on hybrid CPUs
(`/sys/devices/cpu_core/cpus` on Linux).

## vpdq — video hashing

vPDQ video hashing without CGo. `HashFile(ctx, path, opts)` decodes through
an `ffmpeg` subprocess and PDQ-hashes one frame per second (`SecondsPerHash`,
or `EveryFrame`), returning each frame's number, quality, hash and timestamp.
Given x86-64 ffmpeg, the output matches the ThreatExchange C++ vPDQ, and so
HMA's `vpdq`, frame for frame. That needs several settings that differ from
ffmpeg's defaults, each reproducing how the C++ decodes:

- **Colour:** frames are converted with BT.601 coefficients and limited range
  whatever the stream's colour tags say, using the area scaler. The C++ takes
  range only from the pixel format, so full-range `yuvj` streams stay full
  range while a `yuv420p` stream tagged full range does not.
- **Orientation and cropping:** rotation metadata and container-level
  cropping are not applied; codec cropping is.
- **Frame numbering:** every decoded frame of the first video stream counts,
  none dropped or duplicated, and the count carries across a mid-stream
  resolution change. The sampling step is `int(SecondsPerHash * fps)`, with
  fps from `avg_frame_rate`, falling back to `r_frame_rate`.
- **Luma:** the C++ float32 arithmetic (`pdq.CPPLuma()`).
- **Timestamps:** frame number over fps in float32, since formatting a
  float64 of the same division rounds the third decimal differently on some
  frames.
- **Errors:** any decoding error fails the whole file, as it does in the C++.
  Truncation usually is not one, so check a download's length before hashing
  it: a truncated file mostly decodes cleanly to the cut and returns the
  frames before it with no error, as the C++ does. Only a cut inside an MP4
  video sample, whose size the file's index records, fails.

Known differences:

- **Other architectures.** ffmpeg's x86 colour conversion rounds differently
  from its C and arm64 NEON code, so on arm64 hashes land typically ~12 bits
  from the x86 references (up to ~50). That holds for any vPDQ on arm64, the
  C++ included. 10-bit video is unaffected.
- **Mid-stream resolution changes.** Frames after the change are scaled to
  the first frame's size. The C++ output for them is undefined, so they do
  not match.
- **Video not the first stream.** When another stream precedes the video,
  the C++ never flushes its decoder, so it drops the frames the decoder still
  holds at the end (the reorder depth, 2 for typical H.264). This package
  keeps them, so its output is the same whatever the stream order.

`SkipAttachedPictures` selects the first video stream that is not an attached
picture, using ffmpeg's `V` stream specifier, which excludes them. A file
whose only video is cover art, such as an `.m4a`, `.mp3` or `.flac` with
embedded art, then returns `ErrNoVideo` instead of a single hash of the cover.
It is off by default because the C++ hashes the first video stream of any
kind, so it changes the output for those files.

Input is read as a local file with `-protocol_whitelist file` and a
`-format_whitelist` of demuxers that read only their input: the common video
containers, raw and elementary video streams, and mp3, aac, wav and flac so
that audio returns `ErrNoVideo`. Without it, an upload that is an ffconcat
script or HLS playlist would hash the files it names. Other formats return an
error. Frames over 8192x8192 return `ErrFrameTooLarge` before decoding
starts. Each worker holds about 11 bytes per pixel while it hashes a frame
(the RGB24 frame and two float32 buffers), so the default worker count is
capped to keep that under 1 GiB. Frames are always scaled to the first
frame's size, so a later resolution change cannot grow these buffers, though
ffmpeg's own decoder memory is outside that bound.

The hashes depend on ffmpeg's colour conversion, which was identical across
the builds tested on x86-64: shared 8.1.2 and static 8.1.1 and 9.0.2. The
tests check every frame of eight fixture videos against the C++ output,
covering colour tags, full range, rotation, HEVC 10-bit, AV1, 29.97 fps and
variable frame rate, plus stream order, a truncated file, and concat and
playlist inputs. CI installs a pinned static ffmpeg 9.0.2 with
`scripts/install-ffmpeg.sh`. Locally the tests skip without ffmpeg unless
`VPDQ_REQUIRE_FFMPEG` is set, and the exact comparison skips off x86-64.

## Fixtures and provenance

Test fixtures are either generated by committed scripts or sourced from
ThreatExchange, and each directory records its own provenance:
[testdata/README.md](testdata/README.md) (the bridge photograph and the
synthetic reference cases) and
[vpdq/testdata/README.md](vpdq/testdata/README.md) (the doorknob video lineage
and the C++ reference output). ThreatExchange content is used under its
BSD-3-Clause license; see the LICENSE.ThreatExchange.txt files next to the
fixtures.

## Security

Perceptual hashes are similarity signals, not cryptographic commitments, and
the `HMACompatible` option trades evasion resistance for corpus
compatibility. Read [SECURITY.md](SECURITY.md) before using this library for
enforcement.

## License

BSD-3-Clause — see [LICENSE](LICENSE). The implementations reproduce the
output of Meta's PDQ and vPDQ reference implementations
(facebook/ThreatExchange, BSD-3-Clause); see [NOTICE](NOTICE).
