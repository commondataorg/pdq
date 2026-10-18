package pdq

import "math"

const (
	dctRows = 16
	dctCols = 64
)

// dctMatrix is the precomputed DCT matrix for the 64x64 to 16x16 transform.
// Element [i][j] = sqrt(2/64) * cos((PI/128) * (i+1) * (2*j+1))
// This matrix is used to compute B = D * A * D^T where A is the 64x64 input.
var dctMatrix [dctRows][dctCols]float32

func init() {
	matrixScaleFactor := float32(math.Sqrt(2.0 / float64(dctCols)))
	for i := range dctRows {
		for j := range dctCols {
			dctMatrix[i][j] = float32(
				float64(matrixScaleFactor) * math.Cos((math.Pi/2.0/float64(dctCols))*float64(i+1)*float64(2*j+1)))
		}
	}
}

// mul32 returns x*y as a separate function call so the surrounding
// add-multiply pattern is not visible to the SSA optimizer. Without this,
// Go's arm64 backend rewrites `sum += a*b` into a single FMADDS
// instruction, and the resulting ULP-scale rounding difference makes the
// scalar DCT drift from the non-fused C++ ThreatExchange reference. Keep
// this helper and the noinline pragma; inlining defeats the barrier.
//
//go:noinline
func mul32(x, y float32) float32 { return x * y }

// dct64To16Scalar is the scalar (non-SIMD) implementation of the DCT transform.
// It computes B = D * A * D^T where D is the 16x64 DCT matrix.
func dct64To16Scalar(a *[64][64]float32, t *[16][64]float32, b *[16][16]float32) {
	// Compute T = D * A
	// T[i][j] = sum over k of D[i][k] * A[k][j]
	for i := range dctRows {
		for j := range dctCols {
			var sumk float32
			for k := range dctCols {
				sumk += mul32(dctMatrix[i][k], a[k][j])
			}
			t[i][j] = sumk
		}
	}

	// Compute B = T * D^T
	// B[i][j] = sum over k of T[i][k] * D[j][k]
	for i := range dctRows {
		for j := range dctRows {
			var sumk float32
			for k := range dctCols {
				sumk += mul32(t[i][k], dctMatrix[j][k])
			}
			b[i][j] = sumk
		}
	}
}
