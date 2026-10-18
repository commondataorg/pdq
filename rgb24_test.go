package pdq

import (
	"math"
	"math/rand"
	"testing"
)

// lumaDifferences are RGB triples where the two lumas differ in the last bit.
var lumaDifferences = []struct {
	r, g, b         uint8
	cppBits, pyBits uint32
}{
	{0, 11, 77, 0x4173c290, 0x4173c28f},
	{1, 1, 8, 0x3fe624de, 0x3fe624dd},
	{17, 5, 52, 0x415f22d0, 0x415f22d1},
	{64, 3, 85, 0x41f4b22c, 0x41f4b22d},
	{128, 6, 170, 0x4274b22c, 0x4274b22d},
	{200, 2, 214, 0x42aabd70, 0x42aabd71},
}

// TestLumaFromRGB8Float32_MatchesCPP pins triples where the ThreatExchange
// C++ luma and pdqhash's differ in the last bit. The bit patterns came from
// the C++ fillFloatLumaFromRGB, which the float32 luma matched on all 2^24
// RGB triples.
func TestLumaFromRGB8Float32_MatchesCPP(t *testing.T) {
	for _, tc := range lumaDifferences {
		if got := math.Float32bits(lumaFromRGB8Float32(tc.r, tc.g, tc.b)); got != tc.cppBits {
			t.Errorf("C++ luma(%d,%d,%d) = %#08x, want %#08x", tc.r, tc.g, tc.b, got, tc.cppBits)
		}
		if got := math.Float32bits(lumaFromRGB8(tc.r, tc.g, tc.b)); got != tc.pyBits {
			t.Errorf("pdqhash luma(%d,%d,%d) = %#08x, want %#08x", tc.r, tc.g, tc.b, got, tc.pyBits)
		}
	}
}

// TestFillLumaFromRGB24_SelectsLuma checks each option reaches the luma, on
// pixels where the two arithmetics differ.
func TestFillLumaFromRGB24_SelectsLuma(t *testing.T) {
	rgb := make([]byte, 0, 3*len(lumaDifferences))
	for _, tc := range lumaDifferences {
		rgb = append(rgb, tc.r, tc.g, tc.b)
	}
	cpp, py := make([]float32, len(lumaDifferences)), make([]float32, len(lumaDifferences))
	fillLumaFromRGB24(rgb, cpp, true)
	fillLumaFromRGB24(rgb, py, false)
	for i, tc := range lumaDifferences {
		if got := math.Float32bits(cpp[i]); got != tc.cppBits {
			t.Errorf("C++ luma of (%d,%d,%d) = %#08x, want %#08x", tc.r, tc.g, tc.b, got, tc.cppBits)
		}
		if got := math.Float32bits(py[i]); got != tc.pyBits {
			t.Errorf("default luma of (%d,%d,%d) = %#08x, want %#08x", tc.r, tc.g, tc.b, got, tc.pyBits)
		}
	}
}

func TestComputeHashFromRGB24(t *testing.T) {
	const rows, cols = 50, 70
	rng := rand.New(rand.NewSource(4))
	rgb := make([]byte, 3*rows*cols)
	rng.Read(rgb)
	rr, gg, bb := make([]uint8, rows*cols), make([]uint8, rows*cols), make([]uint8, rows*cols)
	for k := range rows * cols {
		rr[k], gg[k], bb[k] = rgb[3*k], rgb[3*k+1], rgb[3*k+2]
	}

	planar, err := ComputeHashFromRGBBytes(rr, gg, bb, rows, cols)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ComputeHashFromRGB24(rgb, rows, cols)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *planar {
		t.Errorf("default luma = %+v, ComputeHashFromRGBBytes = %+v", *got, *planar)
	}

	if _, err := ComputeHashFromRGB24(rgb, rows, cols, CPPLuma()); err != nil {
		t.Errorf("CPPLuma: %v", err)
	}
	if _, err := ComputeHashFromRGB24(rgb[:len(rgb)-1], rows, cols, CPPLuma()); err == nil {
		t.Error("short buffer: want an error")
	}
	if _, err := ComputeHashFromRGB24(rgb[:3*4*4], 4, 4, CPPLuma()); err != ErrImageTooSmall {
		t.Errorf("4x4: err = %v, want ErrImageTooSmall", err)
	}
}
