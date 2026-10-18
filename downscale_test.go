package pdq

import (
	"math"
	"math/rand"
	"testing"
)

func jbuf() ([]float32, []float32, int, int) {
	rows, cols := 3000, 4000
	a := make([]float32, rows*cols)
	b := make([]float32, rows*cols)
	rng := rand.New(rand.NewSource(3))
	for i := range a {
		a[i] = float32(rng.Intn(256))
	}
	return a, b, rows, cols
}

func BenchmarkBoxRowsScalar(bb *testing.B) {
	a, b, rows, cols := jbuf()
	w := computeJaroszFilterWindowSize(cols, 64)
	for bb.Loop() {
		boxAlongRowsScalarFloat(a, b, rows, cols, w)
	}
}

func BenchmarkBoxRows(bb *testing.B) {
	a, b, rows, cols := jbuf()
	w := computeJaroszFilterWindowSize(cols, 64)
	for bb.Loop() {
		boxAlongRowsFloat(a, b, rows, cols, w)
	}
}

func BenchmarkBoxColsScalar(bb *testing.B) {
	a, b, rows, cols := jbuf()
	w := computeJaroszFilterWindowSize(rows, 64)
	for bb.Loop() {
		boxAlongColsScalarFloat(a, b, rows, cols, w)
	}
}

func BenchmarkBoxColsSweep(bb *testing.B) {
	a, b, rows, cols := jbuf()
	w := computeJaroszFilterWindowSize(rows, 64)
	for bb.Loop() {
		boxAlongColsSweepFloat(a, b, rows, cols, w)
	}
}

func BenchmarkBoxCols(bb *testing.B) {
	a, b, rows, cols := jbuf()
	w := computeJaroszFilterWindowSize(rows, 64)
	for bb.Loop() {
		boxAlongColsFloat(a, b, rows, cols, w)
	}
}

// TestBoxAlongCols_MatchesScalar pins that the sweep and dispatched column
// filters are bit-identical to the single-column scalar filter across a range
// of widths (including non-multiples of the vector size) and window sizes.
func TestBoxAlongCols_MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for _, dims := range [][2]int{
		{64, 64}, {100, 100}, {3000, 4000}, {17, 19}, {64, 1}, {64, 7}, {64, 9}, {64, 100}, {200, 333},
		{64, 15}, {64, 16}, {64, 17}, {64, 31}, {64, 32}, {64, 33}, {64, 63}, {50, 47},
	} {
		rows, cols := dims[0], dims[1]
		in := make([]float32, rows*cols)
		for i := range in {
			in[i] = float32(rng.Intn(1000)) + rng.Float32()
		}
		for _, w := range []int{1, 2, 3, 8, computeJaroszFilterWindowSize(rows, 64)} {
			if w < 1 || w > rows {
				continue
			}
			want := make([]float32, rows*cols)
			boxAlongColsScalarFloat(in, want, rows, cols, w)
			for name, f := range map[string]func(in, out []float32, numRows, numCols, windowSize int){
				"sweep":      boxAlongColsSweepFloat,
				"dispatched": boxAlongColsFloat,
			} {
				got := make([]float32, rows*cols)
				f(in, got, rows, cols, w)
				for i := range want {
					if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
						t.Fatalf("%s rows=%d cols=%d w=%d: idx %d: got=%v scalar=%v", name, rows, cols, w, i, got[i], want[i])
					}
				}
			}
		}
	}
}

// TestBoxAlongRows_MatchesScalar pins that the interleaved row filter is
// bit-identical to the one-row-at-a-time scalar filter, including heights
// that are not a multiple of four.
func TestBoxAlongRows_MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for _, dims := range [][2]int{
		{64, 64}, {100, 100}, {3000, 4000}, {19, 17}, {1, 64}, {7, 64}, {15, 64}, {16, 64},
		{17, 64}, {31, 64}, {32, 64}, {33, 64}, {47, 50}, {333, 200}, {16, 1}, {40, 3},
	} {
		rows, cols := dims[0], dims[1]
		in := make([]float32, rows*cols)
		for i := range in {
			in[i] = float32(rng.Intn(1000)) + rng.Float32()
		}
		for _, w := range []int{1, 2, 3, 8, computeJaroszFilterWindowSize(cols, 64)} {
			if w < 1 || w > cols {
				continue
			}
			want := make([]float32, rows*cols)
			boxAlongRowsScalarFloat(in, want, rows, cols, w)
			got := make([]float32, rows*cols)
			boxAlongRowsFloat(in, got, rows, cols, w)
			for i := range want {
				if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
					t.Fatalf("rows=%d cols=%d w=%d: idx %d: got=%v scalar=%v", rows, cols, w, i, got[i], want[i])
				}
			}
		}
	}
}
