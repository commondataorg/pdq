package pdq

import (
	"encoding/json"
	"os"
	"testing"
)

type hmaCase struct {
	File    string `json:"file"`
	Mode    string `json:"mode"`
	HashHex string `json:"hash_hex"`
	Quality int    `json:"quality"`
	Lossy   bool   `json:"lossy"`
}

// maxLossyDistance bounds how far a lossy-decoded image may land from the
// fixture: Go's JPEG and WebP decoders round a few pixel levels differently,
// which moves a few hash bits at most.
const maxLossyDistance = 8

// TestCrossValidationHMA checks ComputeHashFromFile with HMACompatible against
// testdata/hma/hashes.json, which testdata/hma/generate.py produces. Lossless
// formats must match hash and quality exactly; lossy ones within
// maxLossyDistance bits.
func TestCrossValidationHMA(t *testing.T) {
	raw, err := os.ReadFile(testdataPath("hma/hashes.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Cases []hmaCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.File, func(t *testing.T) {
			got, err := ComputeHashFromFile(testdataPath("hma/"+tc.File), HMACompatible())
			if err != nil {
				t.Fatalf("ComputeHashFromFile: %v", err)
			}
			dist, err := HammingDistance(got.Data, tc.HashHex)
			if err != nil {
				t.Fatalf("HammingDistance: %v", err)
			}
			if tc.Lossy {
				if dist > maxLossyDistance {
					t.Errorf("mode %s: %d bits from HMA (max %d)", tc.Mode, dist, maxLossyDistance)
				}
				return
			}
			if dist != 0 {
				t.Errorf("mode %s: hash %s, HMA %s (%d bits)", tc.Mode, got.Data, tc.HashHex, dist)
			}
			if got.Quality != tc.Quality {
				t.Errorf("mode %s: quality %d, HMA %d", tc.Mode, got.Quality, tc.Quality)
			}
		})
	}
}
