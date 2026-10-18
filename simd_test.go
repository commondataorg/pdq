package pdq

import (
	"math"
	"math/rand"
	"testing"
)

// TestDCTOutputWrapper tests that dct64To16 produces correct output.
func TestDCTOutputWrapper(t *testing.T) {
	// Create test input with known pattern
	var a [64][64]float32
	for i := range 64 {
		for j := range 64 {
			a[i][j] = float32(i*64 + j)
		}
	}

	var t1 [16][64]float32
	var b [16][16]float32
	dct64To16(&a, &t1, &b)

	// Just verify output is not all zeros
	var sum float32
	for i := range 16 {
		for j := range 16 {
			sum += abs32(b[i][j])
		}
	}

	if sum == 0 {
		t.Error("DCT output is all zeros")
	}

	t.Logf("DCT output sum of absolute values: %f", sum)
}

// TestDCTMatchesScalar pins that dct64To16, vectorized or not, is
// bit-identical to dct64To16Scalar in both the intermediate and final matrix.
func TestDCTMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	inputs := make([]*[64][64]float32, 0, 101)
	var pattern [64][64]float32
	for i := range 64 {
		for j := range 64 {
			pattern[i][j] = float32(math.Sin(float64(i*7+j*11) / 100.0))
		}
	}
	inputs = append(inputs, &pattern)
	for range 100 {
		var a [64][64]float32
		for i := range 64 {
			for j := range 64 {
				a[i][j] = rng.Float32()*510 - 255
			}
		}
		inputs = append(inputs, &a)
	}

	for n, a := range inputs {
		var tScalar, tGot [16][64]float32
		var bScalar, bGot [16][16]float32
		dct64To16Scalar(a, &tScalar, &bScalar)
		dct64To16(a, &tGot, &bGot)
		for i := range 16 {
			for j := range 64 {
				if math.Float32bits(tGot[i][j]) != math.Float32bits(tScalar[i][j]) {
					t.Fatalf("input %d: T[%d][%d] = %v, scalar %v", n, i, j, tGot[i][j], tScalar[i][j])
				}
			}
			for j := range 16 {
				if math.Float32bits(bGot[i][j]) != math.Float32bits(bScalar[i][j]) {
					t.Fatalf("input %d: B[%d][%d] = %v, scalar %v", n, i, j, bGot[i][j], bScalar[i][j])
				}
			}
		}
	}
}

// BenchmarkDCTScalar benchmarks the scalar DCT implementation.
func BenchmarkDCTScalar(b *testing.B) {
	var a [64][64]float32
	for i := range 64 {
		for j := range 64 {
			a[i][j] = float32(math.Sin(float64(i*7+j*11) / 100.0))
		}
	}

	var t [16][64]float32
	var out [16][16]float32

	b.ResetTimer()
	for b.Loop() {
		dct64To16Scalar(&a, &t, &out)
	}
}

// BenchmarkDCT benchmarks dct64To16, vectorized when built with
// GOEXPERIMENT=simd.
func BenchmarkDCT(b *testing.B) {
	var a [64][64]float32
	for i := range 64 {
		for j := range 64 {
			a[i][j] = float32(math.Sin(float64(i*7+j*11) / 100.0))
		}
	}

	var t [16][64]float32
	var out [16][16]float32

	b.ResetTimer()
	for b.Loop() {
		dct64To16(&a, &t, &out)
	}
}

// BenchmarkComputeHashFromLuma benchmarks hash computation excluding file I/O and decode.
func BenchmarkComputeHashFromLuma(b *testing.B) {
	// Create a synthetic 1024x1024 image's luma values
	numRows, numCols := 1024, 1024
	luma := make([]float32, numRows*numCols)
	for i := range luma {
		luma[i] = float32(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		computeHashFromLuma(luma, numRows, numCols)
	}
}

// BenchmarkComputeHashFromLuma64x64 benchmarks hash computation on pre-downsampled 64x64 input.
// This isolates DCT + quality + median performance.
func BenchmarkComputeHashFromLuma64x64(b *testing.B) {
	// Create a 64x64 image (no Jarosz filter needed)
	numRows, numCols := 64, 64
	luma := make([]float32, numRows*numCols)
	for i := range luma {
		luma[i] = float32(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		computeHashFromLuma(luma, numRows, numCols)
	}
}

// BenchmarkComputeHashFromLuma64x64Scalar benchmarks the scalar DCT on the
// same 64x64 input.
func BenchmarkComputeHashFromLuma64x64Scalar(b *testing.B) {
	// Create a 64x64 image
	var buffer64x64 [64][64]float32
	for i := range 64 {
		for j := range 64 {
			buffer64x64[i][j] = float32((i*64 + j) % 256)
		}
	}

	var buffer16x64 [16][64]float32
	var buffer16x16 [16][16]float32

	b.ResetTimer()
	for b.Loop() {
		dct64To16Scalar(&buffer64x64, &buffer16x64, &buffer16x16)
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
