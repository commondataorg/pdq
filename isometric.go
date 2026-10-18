package pdq

import (
	"encoding/base64"
	"fmt"
	"image"
)

// dihedralCount is the number of dihedral (D4) orientations: identity plus 7
// non-identity variants.
const dihedralCount = 8

// DihedralHashes holds the canonical PDQ hash and the 7 non-identity dihedral
// variant hashes of an image, all base64-encoded. The variants are in PDQ
// order: rot90, rot180, rot270, flipHorizontal, flipVertical, transpose,
// transverse.
type DihedralHashes struct {
	// Hash is the base64 canonical PDQ hash (identity orientation).
	Hash string
	// Quality is the canonical hash quality score (0-100).
	Quality int
	// Isometric holds the 7 non-identity dihedral variant hashes, base64-encoded.
	Isometric []string
}

// IsometricVariant is one dihedral variant hash tagged with its PDQ dihedral
// id (1..7: rot90, rot180, rot270, flipHorizontal, flipVertical, transpose,
// transverse — see permuteLuma).
type IsometricVariant struct {
	ID   int
	Hash string
}

// ComputeHashWithIsometricVariants computes the canonical PDQ hash plus the
// requested dihedral variant hashes of img in a single pass over the source
// pixels. ids selects which non-identity dihedral variants to produce (each in
// 1..7); duplicates are ignored and the returned variants are ascending by id.
// An empty ids returns only the canonical hash and quality.
//
// As in ComputeHashWithIsometric, the per-pixel luminance buffer — the most
// expensive stage — is extracted once and reused for every requested
// orientation (each variant is a pure index remap of that buffer followed by
// the standard downsample+DCT, bit-identical to hashing the separately
// transformed image). Selecting a subset therefore skips only the per-variant
// downsample+DCT work for the orientations left out.
func ComputeHashWithIsometricVariants(img image.Image, ids []int, opts ...Option) (canonical string, quality int, variants []IsometricVariant, err error) {
	var want [dihedralCount]bool
	for _, id := range ids {
		if id < 1 || id >= dihedralCount {
			return "", 0, nil, fmt.Errorf("pdq: invalid dihedral variant id %d (want 1..%d)", id, dihedralCount-1)
		}
		want[id] = true
	}

	b := img.Bounds()
	rows, cols := b.Dy(), b.Dx()
	if rows < MinHashableDim || cols < MinHashableDim {
		return "", 0, nil, ErrImageTooSmall
	}

	luma := make([]float32, rows*cols)
	fillLuma(img, luma, rows, cols, applyOptions(opts))

	canon, q := computeHashFromLuma(luma, rows, cols)
	canonical = base64.StdEncoding.EncodeToString(canon[:])
	quality = q

	// Reusable destination buffer for the permuted luminance; every orientation
	// has the same element count as the source, so one allocation suffices. It
	// is allocated lazily so an empty id set costs nothing beyond the canonical.
	var scratch []float32
	for id := 1; id < dihedralCount; id++ {
		if !want[id] {
			continue
		}
		if scratch == nil {
			scratch = make([]float32, rows*cols)
		}
		pr, pc := permuteLuma(luma, scratch, rows, cols, id)
		variant, _ := computeHashFromLuma(scratch, pr, pc)
		variants = append(variants, IsometricVariant{ID: id, Hash: base64.StdEncoding.EncodeToString(variant[:])})
	}
	return canonical, quality, variants, nil
}

// ComputeHashWithIsometric computes the canonical PDQ hash plus all 7 dihedral
// variant hashes of img. It is a thin wrapper over
// ComputeHashWithIsometricVariants requesting the full dihedral set; the
// variants populate Isometric in PDQ id order (rot90, rot180, rot270,
// flipHorizontal, flipVertical, transpose, transverse), matching the prior
// behavior bit-for-bit.
func ComputeHashWithIsometric(img image.Image, opts ...Option) (*DihedralHashes, error) {
	canonical, quality, variants, err := ComputeHashWithIsometricVariants(img, []int{1, 2, 3, 4, 5, 6, 7}, opts...)
	if err != nil {
		return nil, err
	}
	iso := make([]string, len(variants))
	for i, v := range variants {
		iso[i] = v.Hash
	}
	return &DihedralHashes{Hash: canonical, Quality: quality, Isometric: iso}, nil
}

// permuteLuma writes the dihedral transform id of the rows*cols source
// luminance buffer into dst and returns dst's dimensions (luminance is
// per-pixel, so permuting the luma buffer equals hashing the transformed
// image). id: 1=rot90, 2=rot180, 3=rot270, 4=flipHorizontal, 5=flipVertical,
// 6=transpose, 7=transverse.
func permuteLuma(src, dst []float32, rows, cols, id int) (int, int) {
	at := func(r, c int) float32 { return src[r*cols+c] }
	var nr, nc int
	var get func(r, c int) float32
	switch id {
	case 1: // rot90 CW
		nr, nc = cols, rows
		get = func(r, c int) float32 { return at(rows-1-c, r) }
	case 2: // rot180
		nr, nc = rows, cols
		get = func(r, c int) float32 { return at(rows-1-r, cols-1-c) }
	case 3: // rot270 CW
		nr, nc = cols, rows
		get = func(r, c int) float32 { return at(c, cols-1-r) }
	case 4: // flip horizontal
		nr, nc = rows, cols
		get = func(r, c int) float32 { return at(r, cols-1-c) }
	case 5: // flip vertical
		nr, nc = rows, cols
		get = func(r, c int) float32 { return at(rows-1-r, c) }
	case 6: // transpose
		nr, nc = cols, rows
		get = func(r, c int) float32 { return at(c, r) }
	case 7: // transverse
		nr, nc = cols, rows
		get = func(r, c int) float32 { return at(rows-1-c, cols-1-r) }
	}
	for r := range nr {
		for c := range nc {
			dst[r*nc+c] = get(r, c)
		}
	}
	return nr, nc
}
