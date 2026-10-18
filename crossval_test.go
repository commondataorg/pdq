package pdq

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type referenceTestCase struct {
	Name    string  `json:"name"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	R       []uint8 `json:"r"`
	G       []uint8 `json:"g"`
	B       []uint8 `json:"b"`
	HashHex string  `json:"hash_hex"`
	Quality int     `json:"quality"`
}

type referenceData struct {
	TestCases []referenceTestCase `json:"test_cases"`
}

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func loadReferenceData(t *testing.T) *referenceData {
	t.Helper()
	path := testdataPath("reference_hashes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", path, err)
	}
	var ref referenceData
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatalf("Failed to parse reference_hashes.json: %v", err)
	}
	return &ref
}

// TestCrossValidationRGBBytes verifies bit-identical output against pdqhash-python
// reference hashes for 24 synthetic test images. Uses ComputeHashFromRGBBytes
// with raw uint8 pixel data, so JPEG decoder differences are not a factor.
func TestCrossValidationRGBBytes(t *testing.T) {
	ref := loadReferenceData(t)

	for _, tc := range ref.TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			hash, err := ComputeHashFromRGBBytes(tc.R, tc.G, tc.B, tc.Height, tc.Width)
			if err != nil {
				t.Fatalf("ComputeHashFromRGBBytes failed: %v", err)
			}

			if hash.Data != tc.HashHex {
				dist, _ := HammingDistance(hash.Data, tc.HashHex)
				t.Errorf("hash mismatch (hamming distance %d):\n  got:  %s\n  want: %s",
					dist, hash.Data, tc.HashHex)
			}

			if hash.Quality != tc.Quality {
				t.Errorf("quality mismatch: got %d, want %d", hash.Quality, tc.Quality)
			}
		})
	}
}

// TestAPIConsistencyRGBvsImage verifies that ComputeHash(image.Image) and
// ComputeHashFromRGBBytes produce identical hashes and quality scores for the
// same pixel data. Both paths use scalar DCT so they must match exactly.
func TestAPIConsistencyRGBvsImage(t *testing.T) {
	ref := loadReferenceData(t)

	for _, tc := range ref.TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			hashRGB, err := ComputeHashFromRGBBytes(tc.R, tc.G, tc.B, tc.Height, tc.Width)
			if err != nil {
				t.Fatalf("ComputeHashFromRGBBytes failed: %v", err)
			}

			img := rgbToImage(tc.R, tc.G, tc.B, tc.Width, tc.Height)

			hashImg, err := ComputeHash(img)
			if err != nil {
				t.Fatalf("ComputeHash failed: %v", err)
			}

			if hashImg.Data != hashRGB.Data {
				dist, _ := HammingDistance(hashImg.Data, hashRGB.Data)
				t.Errorf("hash mismatch (hamming distance %d):\n  ComputeHash:            %s\n  ComputeHashFromRGBBytes: %s",
					dist, hashImg.Data, hashRGB.Data)
			}

			if hashImg.Quality != hashRGB.Quality {
				t.Errorf("quality differs: ComputeHash=%d ComputeHashFromRGBBytes=%d",
					hashImg.Quality, hashRGB.Quality)
			}
		})
	}
}

func rgbToImage(rr, gg, bb []uint8, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range h {
		for j := range w {
			k := i*w + j
			off := img.PixOffset(j, i)
			img.Pix[off+0] = rr[k]
			img.Pix[off+1] = gg[k]
			img.Pix[off+2] = bb[k]
			img.Pix[off+3] = 0xFF
		}
	}
	return img
}
