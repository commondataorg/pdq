# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "threatexchange==1.2.16",
#     "pdqhash==0.2.8",
#     "pillow==12.3.0",
#     "numpy==2.5.3",
# ]
# ///
"""Time pdqhash-python on testdata/bridge.jpg, for the README's comparison.

    taskset -c 0 uv run scripts/bench_pdqhash.py

Counterparts of BenchmarkComputeHashFromFileHMA and
BenchmarkComputeHashFromRGB24. Prints the median of each.
"""

import statistics
import time
from collections.abc import Callable
from pathlib import Path

import numpy as np
import pdqhash
from PIL import Image
from threatexchange.signal_type.pdq.pdq_hasher import pdq_from_file

IMAGE = Path(__file__).resolve().parent.parent / "testdata" / "bridge.jpg"
WARMUP = 10
RUNS = 200


def median_ms(fn: Callable[[], object]) -> float:
    for _ in range(WARMUP):
        fn()
    samples = []
    for _ in range(RUNS):
        start = time.perf_counter_ns()
        fn()
        samples.append(time.perf_counter_ns() - start)
    return statistics.median(samples) / 1e6


def main() -> None:
    rgb = np.ascontiguousarray(np.asarray(Image.open(IMAGE).convert("RGB")))
    assert rgb.dtype == np.uint8 and rgb.ndim == 3 and rgb.shape[2] == 3

    print(f"pdq_from_file:   {median_ms(lambda: pdq_from_file(IMAGE)):.2f} ms")
    print(f"pdqhash.compute: {median_ms(lambda: pdqhash.compute(rgb)):.2f} ms")


if __name__ == "__main__":
    main()
