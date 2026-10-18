//go:build goexperiment.simd

package pdq

import "simd"

// The column filter phase loops count explicitly because Go 1.27's simd
// rewriter panics on a `for range n` with no loop variable (golang/go#81264).

// simdHardware is false when the simd package is emulating vectors in scalar
// Go, which is slower than the plain scalar paths.
var simdHardware = !simd.Emulated()

// dctMatrixT is dctMatrix transposed, so the second DCT pass can load
// contiguous vectors across its output columns.
var dctMatrixT [dctCols][dctRows]float32

func init() {
	for i := range dctRows {
		for j := range dctCols {
			dctMatrixT[j][i] = dctMatrix[i][j]
		}
	}
}

// dct64To16 computes B = D * A * D^T, bit-identical to dct64To16Scalar.
func dct64To16(a *[64][64]float32, t *[16][64]float32, b *[16][16]float32) {
	if !simdHardware {
		dct64To16Scalar(a, t, b)
		return
	}
	dct64To16Vector(a, t, b)
}

// dct64To16Vector vectorizes across output columns. Each lane accumulates
// its dot product over k in the same order as the scalar loop, with a
// separate Mul and Add (never MulAdd), so every lane rounds exactly as the
// scalar reference does.
func dct64To16Vector(a *[64][64]float32, t *[16][64]float32, b *[16][16]float32) {
	l := simd.BroadcastFloat32s(0).Len()
	for i := range dctRows {
		for j := 0; j < dctCols; j += l {
			var acc simd.Float32s
			for k := range dctCols {
				acc = acc.Add(simd.BroadcastFloat32s(dctMatrix[i][k]).Mul(simd.LoadFloat32s(a[k][j:])))
			}
			acc.Store(t[i][j:])
		}
	}
	for i := range dctRows {
		for j := 0; j < dctRows; j += l {
			var acc simd.Float32s
			for k := range dctCols {
				acc = acc.Add(simd.BroadcastFloat32s(t[i][k]).Mul(simd.LoadFloat32s(dctMatrixT[k][j:])))
			}
			acc.Store(b[i][j:])
		}
	}
}

// boxAlongColsFloat applies the 1D box filter down each column,
// bit-identical to boxAlongColsScalarFloat.
func boxAlongColsFloat(in, out []float32, numRows, numCols, windowSize int) {
	if !simdHardware {
		boxAlongColsSweepFloat(in, out, numRows, numCols, windowSize)
		return
	}
	boxAlongColsSweepVector(in, out, numRows, numCols, windowSize)
}

// boxAlongColsSweepVector is boxAlongColsSweepFloat with the per-column
// updates vectorized; the columns past the last full vector run scalar. Add,
// Sub and Div are exact IEEE operations per lane, so each column matches
// box1DFloat bit for bit.
func boxAlongColsSweepVector(in, out []float32, numRows, numCols, fullWindowSize int) {
	halfWindowSize := (fullWindowSize + 2) / 2

	phase1Nreps := halfWindowSize - 1
	phase2Nreps := fullWindowSize - halfWindowSize + 1
	phase3Nreps := numRows - fullWindowSize
	phase4Nreps := halfWindowSize - 1

	l := simd.BroadcastFloat32s(0).Len()
	vec := numCols - numCols%l
	sum := make([]float32, numCols)
	row := func(i int) []float32 { return in[i*numCols : (i+1)*numCols] }
	orow := func(i int) []float32 { return out[i*numCols : (i+1)*numCols] }
	li, ri, oi := 0, 0, 0
	currentWindowSize := 0

	for n := 0; n < phase1Nreps; n++ {
		r := row(ri)
		for c := 0; c < vec; c += l {
			simd.LoadFloat32s(sum[c:]).Add(simd.LoadFloat32s(r[c:])).Store(sum[c:])
		}
		for c := vec; c < numCols; c++ {
			sum[c] += r[c]
		}
		currentWindowSize++
		ri++
	}

	for n := 0; n < phase2Nreps; n++ {
		r, o := row(ri), orow(oi)
		currentWindowSize++
		d := float32(currentWindowSize)
		dv := simd.BroadcastFloat32s(d)
		for c := 0; c < vec; c += l {
			s := simd.LoadFloat32s(sum[c:]).Add(simd.LoadFloat32s(r[c:]))
			s.Store(sum[c:])
			s.Div(dv).Store(o[c:])
		}
		for c := vec; c < numCols; c++ {
			sum[c] += r[c]
			o[c] = sum[c] / d
		}
		ri++
		oi++
	}

	d := float32(currentWindowSize)
	dv := simd.BroadcastFloat32s(d)
	for n := 0; n < phase3Nreps; n++ {
		r, lr, o := row(ri), row(li), orow(oi)
		for c := 0; c < vec; c += l {
			s := simd.LoadFloat32s(sum[c:]).Add(simd.LoadFloat32s(r[c:])).Sub(simd.LoadFloat32s(lr[c:]))
			s.Store(sum[c:])
			s.Div(dv).Store(o[c:])
		}
		for c := vec; c < numCols; c++ {
			sum[c] += r[c]
			sum[c] -= lr[c]
			o[c] = sum[c] / d
		}
		li++
		ri++
		oi++
	}

	for n := 0; n < phase4Nreps; n++ {
		lr, o := row(li), orow(oi)
		currentWindowSize--
		d := float32(currentWindowSize)
		dv := simd.BroadcastFloat32s(d)
		for c := 0; c < vec; c += l {
			s := simd.LoadFloat32s(sum[c:]).Sub(simd.LoadFloat32s(lr[c:]))
			s.Store(sum[c:])
			s.Div(dv).Store(o[c:])
		}
		for c := vec; c < numCols; c++ {
			sum[c] -= lr[c]
			o[c] = sum[c] / d
		}
		li++
		oi++
	}
}
