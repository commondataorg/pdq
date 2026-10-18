# pkg/vpdq test fixture

Each `NAME.mp4` has two references from the ThreatExchange C++ vPDQ:
`NAME.every-frame.txt` hashes every frame and `NAME.1s.txt` one frame per
second. Each line is `frame,quality,hash,timestamp`, with the timestamp at 3
decimals. `invalid/` holds inputs the C++ rejects, which have no references.

## Videos

`doorknob.mp4` is `tmk/sample-videos/doorknob-hd-no-bar.mp4` from
[facebook/ThreatExchange](https://github.com/facebook/ThreatExchange),
distributed under `LICENSE.ThreatExchange.txt`. The others are derived from it,
each exercising something the pipeline has to reproduce:

 | File                    | Exercises                                     |
 | ----------------------- | --------------------------------------------- |
 | `bt709.mp4`             | colour tags ignored                           |
 | `av1-fullrange.mp4`     | full-range tag on a `yuv420p` stream ignored  |
 | `rot90.mp4`             | rotation not applied                          |
 | `hevc10-hlg.mp4`        | HEVC, 10-bit, BT.2020 HLG                     |
 | `av1.mp4`               | AV1                                           |
 | `ntsc-29.97.mp4`        | float32 timestamps, step truncation           |
 | `vfr.mp4`               | variable frame rate                           |
 | `invalid/truncated.mp4` | an MP4 cut inside a video sample fails        |

Each was made from `doorknob.mp4` with `ffmpeg -i doorknob.mp4 ARGS OUT`,
except `rot90.mp4`, whose rotation is an input option, and
`invalid/truncated.mp4`, which is its first 300000 bytes:

```sh
# bt709.mp4
-c copy -bsf:v h264_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1
# av1-fullrange.mp4
-c:v libsvtav1 -preset 12 -color_range pc
# rot90.mp4
ffmpeg -display_rotation 90 -i doorknob.mp4 -c copy rot90.mp4
# hevc10-hlg.mp4
-c:v libx265 -pix_fmt yuv420p10le -x265-params colorprim=bt2020:transfer=arib-std-b67:colormatrix=bt2020nc
# av1.mp4
-c:v libsvtav1 -preset 12
# ntsc-29.97.mp4
-vf fps=30000/1001 -c:v libx264 -preset veryfast -crf 30
# vfr.mp4
-vf "select='lt(n\,30)+not(mod(n\,3))'" -fps_mode vfr -c:v libx264 -preset veryfast -crf 30
```

At 30000/1001 fps, frame 75 is exactly 2.5025 s. The C++ computes that in
float32 and prints 2.503; a float64 division prints 2.502. `ntsc-29.97` pins it.

## References

Generated with the ThreatExchange C++ vPDQ reference implementation
(`hashVideoFile`, no downsampling) linked against x86-64 ffmpeg 8.1.2
shared libraries, at 0.001 seconds per hash for every frame and 1.0 for the
one-per-second files. They match exactly under the static x86-64 builds of
8.1.1 and 9.0.2; `scripts/install-ffmpeg.sh` installs 9.0.2 for CI.

vPDQ hashes depend on ffmpeg's colour conversion, which differs by CPU
architecture: arm64 builds land a few to ~50 bits away. Regenerate these with
the C++ vPDQ if CI moves to an ffmpeg that no longer matches them.
