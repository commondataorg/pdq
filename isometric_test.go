package pdq

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// remapVariant returns src transformed by dihedral id (1..7) — the
// image-level equivalent of permuteLuma's luma permutation, for bit-exactness
// cross-checks. id order: 1=rot90,2=rot180,3=rot270,4=flipH,5=flipV,
// 6=transpose,7=transverse.
func remapVariant(src image.Image, id int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dw, dh int
	var fn func(x, y int) (int, int)
	switch id {
	case 1:
		dw, dh, fn = h, w, func(x, y int) (int, int) { return h - 1 - y, x }
	case 2:
		dw, dh, fn = w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y }
	case 3:
		dw, dh, fn = h, w, func(x, y int) (int, int) { return y, w - 1 - x }
	case 4:
		dw, dh, fn = w, h, func(x, y int) (int, int) { return w - 1 - x, y }
	case 5:
		dw, dh, fn = w, h, func(x, y int) (int, int) { return x, h - 1 - y }
	case 6:
		dw, dh, fn = h, w, func(x, y int) (int, int) { return y, x }
	case 7:
		dw, dh, fn = h, w, func(x, y int) (int, int) { return h - 1 - y, w - 1 - x }
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			dx, dy := fn(x, y)
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func isoTestImage(w, h, pattern int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(int64(pattern + 1)))
	for y := range h {
		for x := range w {
			switch pattern {
			case 0:
				img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x + y) % 256), 255})
			case 1:
				v := uint8((x*x + y*y) % 256)
				img.Set(x, y, color.RGBA{v, 255 - v, uint8((int(v) * 3) % 256), 255})
			default:
				img.Set(x, y, color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255})
			}
		}
	}
	return img
}

// TestComputeHashWithIsometric_MatchesPerVariant pins that the luma-share path
// is bit-exact with the current per-variant path (canonical + each of the 7
// dihedral variants) across sizes, shapes, and content.
func TestComputeHashWithIsometric_MatchesPerVariant(t *testing.T) {
	for _, sz := range [][2]int{{1000, 1000}, {1920, 1080}, {600, 800}, {257, 511}, {64, 64}, {65, 64}} {
		for pattern := range 3 {
			img := isoTestImage(sz[0], sz[1], pattern)

			got, err := ComputeHashWithIsometric(img)
			if err != nil {
				t.Fatalf("ComputeHashWithIsometric: %v", err)
			}

			wantCanonical, _, err := ComputeHashBase64(img)
			if err != nil {
				t.Fatalf("canonical: %v", err)
			}
			if got.Hash != wantCanonical {
				t.Errorf("size %v pattern %d: canonical = %s, want %s", sz, pattern, got.Hash, wantCanonical)
			}
			if len(got.Isometric) != 7 {
				t.Fatalf("got %d isometric hashes, want 7", len(got.Isometric))
			}
			for id := 1; id <= 7; id++ {
				want, _, err := ComputeHashBase64(remapVariant(img, id))
				if err != nil {
					t.Fatalf("per-variant %d: %v", id, err)
				}
				if got.Isometric[id-1] != want {
					dist, _ := HammingDistance(got.Isometric[id-1], want)
					t.Errorf("size %v pattern %d variant %d: drift=%d (got %s want %s)",
						sz, pattern, id, dist, got.Isometric[id-1], want)
				}
			}
		}
	}
}

func BenchmarkPerVariantIsometric(b *testing.B) {
	img := isoTestImage(4000, 3000, 1)
	for b.Loop() {
		_, _, _ = ComputeHashBase64(img)
		for id := 1; id <= 7; id++ {
			_, _, _ = ComputeHashBase64(remapVariant(img, id))
		}
	}
}

func BenchmarkLumaShareIsometric(b *testing.B) {
	img := isoTestImage(4000, 3000, 1)
	for b.Loop() {
		_, _ = ComputeHashWithIsometric(img)
	}
}
