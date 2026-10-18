package pdq

import (
	"fmt"
	"image"
	"math/rand"
	"testing"
)

// fakeImage wraps an image.Image so the concrete-type switch in
// fillFloatLumaFast does not match, exercising the generic fallback.
type fakeImage struct{ image.Image }

func lumaGeneric(t *testing.T, img image.Image) []float32 {
	t.Helper()
	b := img.Bounds()
	out := make([]float32, b.Dy()*b.Dx())
	fillFloatLumaGeneric(img, out, b.Dy(), b.Dx())
	return out
}

func lumaDispatch(t *testing.T, img image.Image) []float32 {
	t.Helper()
	b := img.Bounds()
	out := make([]float32, b.Dy()*b.Dx())
	fillFloatLumaFromImage(img, out, b.Dy(), b.Dx())
	return out
}

func TestFillFloatLuma_FastMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const w, h = 67, 53 // odd, non-square to flush offset math

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	rng.Read(rgba.Pix)

	gray := image.NewGray(image.Rect(0, 0, w, h))
	rng.Read(gray.Pix)

	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng.Read(nrgba.Pix) // random alpha exercises the premultiply path

	// Sub-image with a non-zero origin: shares Pix at an offset.
	rgbaBig := image.NewRGBA(image.Rect(0, 0, w+10, h+10))
	rng.Read(rgbaBig.Pix)
	rgbaSub := rgbaBig.SubImage(image.Rect(5, 7, 5+w, 7+h))

	imgs := map[string]image.Image{
		"RGBA":    rgba,
		"Gray":    gray,
		"NRGBA":   nrgba,
		"RGBASub": rgbaSub,
	}
	for _, ratio := range []image.YCbCrSubsampleRatio{
		image.YCbCrSubsampleRatio444,
		image.YCbCrSubsampleRatio422,
		image.YCbCrSubsampleRatio420,
		image.YCbCrSubsampleRatio440,
		image.YCbCrSubsampleRatio411,
		image.YCbCrSubsampleRatio410,
	} {
		yc := image.NewYCbCr(image.Rect(0, 0, w, h), ratio)
		rng.Read(yc.Y)
		rng.Read(yc.Cb)
		rng.Read(yc.Cr)
		imgs["YCbCr_"+ratio.String()] = yc

		// Odd and negative origins, where chroma offsets round differently.
		for _, o := range []image.Point{{5, 7}, {-9, -3}} {
			big := image.NewYCbCr(image.Rect(o.X-3, o.Y-2, o.X+w+4, o.Y+h+5), ratio)
			rng.Read(big.Y)
			rng.Read(big.Cb)
			rng.Read(big.Cr)
			imgs[fmt.Sprintf("YCbCr_%s_sub%d,%d", ratio, o.X, o.Y)] = big.SubImage(image.Rect(o.X, o.Y, o.X+w, o.Y+h))
		}
	}

	for name, img := range imgs {
		t.Run(name, func(t *testing.T) {
			if !fillFloatLumaFast(img, make([]float32, img.Bounds().Dy()*img.Bounds().Dx()), img.Bounds().Dy(), img.Bounds().Dx()) {
				t.Fatalf("expected a fast path for %s, got fallback", name)
			}
			want := lumaGeneric(t, img)
			got := lumaDispatch(t, img)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s: luma[%d] = %v, generic = %v", name, i, got[i], want[i])
				}
			}
		})
	}
}

func TestFillFloatLuma_FallbackForUnknownType(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if fillFloatLumaFast(fakeImage{rgba}, make([]float32, 64), 8, 8) {
		t.Fatal("expected fallback (false) for unrecognized image type")
	}
	// Dispatch must still produce the generic result for the wrapped image.
	want := lumaGeneric(t, rgba)
	got := lumaDispatch(t, fakeImage{rgba})
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fallback luma[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
