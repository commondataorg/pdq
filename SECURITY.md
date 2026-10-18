# Security policy

## Scope

pdq computes perceptual hashes: similarity signals that survive re-encoding.
They are not cryptographic commitments. An adversary who can modify an image
can deliberately move its hash away from any reference — this is inherent to
the algorithm class, not a defect of this implementation. Design
accordingly: pair hashes with a match threshold chosen for the deployment,
and treat matches as leads rather than verdicts.

Two library decisions trade robustness for compatibility, and both matter
for threat modelling:

- The default rendered-colour path resists re-encoding: a permuted palette,
  a CMYK or 16-bit copy leaves the hash where it was. This is what
  enforcement matching should use. `HMACompatible()` instead hashes HMA's
  native-mode values so results string-match HMA-built corpora; those values
  are the uploader's to choose, so palette permutation or CMYK/16-bit
  re-encoding moves this hash far from the original. Do not use it for
  enforcement.
- vpdq's output follows ffmpeg's colour conversion and the ThreatExchange
  C++ decoding behaviour. On arm64 it differs from the x86-64 reference
  fixtures by typically ~12 bits (up to ~50). Pick thresholds for the
  platform the matcher runs on.

Malformed-input handling is part of the security surface and is pinned by
the tests: images over Pillow's decompression-bomb limit (178,956,970 pixels)
are rejected before their pixels are allocated; vpdq reads input only as a
local `file:` URL with `-protocol_whitelist file` and a `-format_whitelist`
limited to demuxers that read only their input (an ffconcat script or HLS
playlist upload hashes nothing but itself); frames over 8192x8192 are
rejected with `ErrFrameTooLarge` before decoding starts.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting on this repository (Security
tab, Report a vulnerability). Include the input that triggers it, the hash
or error produced, and the reference implementation's output if you have it.
Correctness deviations from the documented reference output are in scope:
they can cause missed matches or false positives.
