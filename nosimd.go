//go:build !goexperiment.simd

package pdq

// dct64To16 computes B = D * A * D^T. Build with GOEXPERIMENT=simd for the
// vectorized version.
func dct64To16(a *[64][64]float32, t *[16][64]float32, b *[16][16]float32) {
	dct64To16Scalar(a, t, b)
}

// boxAlongColsFloat applies the 1D box filter down each column.
func boxAlongColsFloat(in, out []float32, numRows, numCols, windowSize int) {
	boxAlongColsSweepFloat(in, out, numRows, numCols, windowSize)
}

// vectorDivide is false: dividing in a separate scalar pass is slower than
// dividing as the sums are made.
const vectorDivide = false

// divideFloats divides each element of v by d.
func divideFloats(v []float32, d float32) {
	for i := range v {
		v[i] /= d
	}
}
