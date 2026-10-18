package pdq

import (
	"slices"
	"testing"
)

// TestComputeHashWithIsometricVariants pins the subset primitive: it computes
// only the requested dihedral ids, returns them id-tagged in ascending order,
// and each variant hash is bit-identical to hashing the separately-transformed
// image (the same per-variant reference TestComputeHashWithIsometric pins).
func TestComputeHashWithIsometricVariants(t *testing.T) {
	img := isoTestImage(257, 511, 2)

	wantCanonical, wantQuality, err := ComputeHashBase64(img)
	if err != nil {
		t.Fatalf("ComputeHashBase64: %v", err)
	}

	t.Run("subset is id-tagged, ascending, and bit-exact per variant", func(t *testing.T) {
		ids := []int{1, 2, 3, 4} // rot90, rot180, rot270, flipH
		canonical, quality, variants, err := ComputeHashWithIsometricVariants(img, ids)
		if err != nil {
			t.Fatalf("ComputeHashWithIsometricVariants: %v", err)
		}
		if canonical != wantCanonical {
			t.Errorf("canonical = %s, want %s", canonical, wantCanonical)
		}
		if quality != wantQuality {
			t.Errorf("quality = %d, want %d", quality, wantQuality)
		}
		if len(variants) != len(ids) {
			t.Fatalf("got %d variants, want %d", len(variants), len(ids))
		}
		for i, id := range ids {
			if variants[i].ID != id {
				t.Errorf("variants[%d].ID = %d, want %d", i, variants[i].ID, id)
			}
			ref, _, err := ComputeHashBase64(remapVariant(img, id))
			if err != nil {
				t.Fatalf("reference variant %d: %v", id, err)
			}
			if variants[i].Hash != ref {
				dist, _ := HammingDistance(variants[i].Hash, ref)
				t.Errorf("variant id %d: drift=%d (got %s want %s)", id, dist, variants[i].Hash, ref)
			}
		}
	})

	t.Run("empty ids yields canonical only", func(t *testing.T) {
		canonical, _, variants, err := ComputeHashWithIsometricVariants(img, nil)
		if err != nil {
			t.Fatalf("ComputeHashWithIsometricVariants: %v", err)
		}
		if canonical != wantCanonical {
			t.Errorf("canonical = %s, want %s", canonical, wantCanonical)
		}
		if len(variants) != 0 {
			t.Errorf("got %d variants, want 0", len(variants))
		}
	})

	t.Run("ids deduped and returned ascending regardless of input order", func(t *testing.T) {
		_, _, variants, err := ComputeHashWithIsometricVariants(img, []int{4, 1, 4})
		if err != nil {
			t.Fatalf("ComputeHashWithIsometricVariants: %v", err)
		}
		gotIDs := make([]int, 0, len(variants))
		for _, v := range variants {
			gotIDs = append(gotIDs, v.ID)
		}
		if want := []int{1, 4}; !slices.Equal(gotIDs, want) {
			t.Errorf("ids = %v, want %v", gotIDs, want)
		}
	})

	t.Run("invalid id errors", func(t *testing.T) {
		for _, bad := range []int{0, 8, -1} {
			if _, _, _, err := ComputeHashWithIsometricVariants(img, []int{bad}); err == nil {
				t.Errorf("id %d: expected error, got nil", bad)
			}
		}
	})

	t.Run("full set matches ComputeHashWithIsometric", func(t *testing.T) {
		_, _, variants, err := ComputeHashWithIsometricVariants(img, []int{1, 2, 3, 4, 5, 6, 7})
		if err != nil {
			t.Fatalf("ComputeHashWithIsometricVariants: %v", err)
		}
		full, err := ComputeHashWithIsometric(img)
		if err != nil {
			t.Fatalf("ComputeHashWithIsometric: %v", err)
		}
		if len(variants) != 7 {
			t.Fatalf("got %d variants, want 7", len(variants))
		}
		for i, v := range variants {
			if v.ID != i+1 {
				t.Errorf("variants[%d].ID = %d, want %d", i, v.ID, i+1)
			}
			if v.Hash != full.Isometric[i] {
				t.Errorf("variant id %d hash != ComputeHashWithIsometric.Isometric[%d]", v.ID, i)
			}
		}
	})

	t.Run("image too small errors", func(t *testing.T) {
		if _, _, _, err := ComputeHashWithIsometricVariants(isoTestImage(4, 4, 0), []int{1}); err == nil {
			t.Error("expected ErrImageTooSmall, got nil")
		}
	})

	t.Run("minimum-size image computes a subset bit-exactly", func(t *testing.T) {
		small := isoTestImage(MinHashableDim, MinHashableDim, 0)
		ids := []int{1, 4} // rot90, flipH
		_, _, variants, err := ComputeHashWithIsometricVariants(small, ids)
		if err != nil {
			t.Fatalf("ComputeHashWithIsometricVariants(%dx%d): %v", MinHashableDim, MinHashableDim, err)
		}
		if len(variants) != len(ids) {
			t.Fatalf("got %d variants, want %d", len(variants), len(ids))
		}
		for i, id := range ids {
			if variants[i].ID != id {
				t.Errorf("variants[%d].ID = %d, want %d", i, variants[i].ID, id)
			}
			ref, _, err := ComputeHashBase64(remapVariant(small, id))
			if err != nil {
				t.Fatalf("reference variant %d: %v", id, err)
			}
			if variants[i].Hash != ref {
				t.Errorf("variant id %d: %dx%d hash mismatch (got %s want %s)",
					id, MinHashableDim, MinHashableDim, variants[i].Hash, ref)
			}
		}
	})
}
