package pdq

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

func lumaHMA(t *testing.T, img image.Image) []float32 {
	t.Helper()
	b := img.Bounds()
	out := make([]float32, b.Dy()*b.Dx())
	fillFloatLumaHMA(img, out, b.Dy(), b.Dx())
	return out
}

// unpremultiply is the straight colour HMACompatible reads from a
// premultiplied channel: 0 at zero alpha, v*255/a truncated and clipped.
func unpremultiply(v, a uint8) uint8 {
	switch a {
	case 0:
		return 0
	case 255:
		return v
	}
	return uint8(min(int(v)*255/int(a), 255))
}

// TestFillFloatLumaHMA_PixelValues pins the samples HMACompatible hashes for
// the types where they differ from rendered colour: straight colour with
// alpha ignored (premultiplied types unpremultiplied), palette indices, CMYK
// channels read as RGB, the high byte of 16-bit colour, and 16-bit grey
// clipped at 255.
func TestFillFloatLumaHMA_PixelValues(t *testing.T) {
	const w, h = 67, 53 // odd, non-square to flush offset math
	rng := rand.New(rand.NewSource(2))
	rect := image.Rect(0, 0, w, h)
	n := w * h
	want := func(f func(k int) (r, g, b uint8)) []float32 {
		out := make([]float32, n)
		for k := range n {
			r, g, b := f(k)
			out[k] = lumaFromRGB8(r, g, b)
		}
		return out
	}

	rgba := image.NewRGBA(rect)
	rng.Read(rgba.Pix)
	nrgba := image.NewNRGBA(rect)
	rng.Read(nrgba.Pix)
	nrgba64 := image.NewNRGBA64(rect)
	rng.Read(nrgba64.Pix)
	rgba64 := image.NewRGBA64(rect)
	rng.Read(rgba64.Pix)
	cmyk := image.NewCMYK(rect)
	rng.Read(cmyk.Pix)

	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.RGBA{R: uint8(255 - i), G: uint8(i / 2), B: uint8(i * 3), A: 255}
	}
	paletted := image.NewPaletted(rect, palette)
	rng.Read(paletted.Pix)

	gray16 := image.NewGray16(rect)
	for k := range n {
		// Mostly below 256, so both sides of the clip are exercised.
		gray16.SetGray16(k%w, k/w, color.Gray16{Y: uint16(rng.Intn(600))})
	}

	nycbcra := image.NewNYCbCrA(rect, image.YCbCrSubsampleRatio420)
	rng.Read(nycbcra.Y)
	rng.Read(nycbcra.Cb)
	rng.Read(nycbcra.Cr)
	rng.Read(nycbcra.A)

	gray := image.NewGray(rect)
	rng.Read(gray.Pix)

	cases := map[string]struct {
		img  image.Image
		want []float32
	}{
		"RGBA": {rgba, want(func(k int) (uint8, uint8, uint8) {
			p, a := rgba.Pix[4*k:4*k+3], rgba.Pix[4*k+3]
			return unpremultiply(p[0], a), unpremultiply(p[1], a), unpremultiply(p[2], a)
		})},
		"NRGBA": {nrgba, want(func(k int) (uint8, uint8, uint8) {
			return nrgba.Pix[4*k], nrgba.Pix[4*k+1], nrgba.Pix[4*k+2]
		})},
		"NRGBA64": {nrgba64, want(func(k int) (uint8, uint8, uint8) {
			return nrgba64.Pix[8*k], nrgba64.Pix[8*k+2], nrgba64.Pix[8*k+4]
		})},
		"RGBA64": {rgba64, want(func(k int) (uint8, uint8, uint8) {
			p, a := rgba64.Pix[8*k:8*k+6], rgba64.Pix[8*k+6]
			return unpremultiply(p[0], a), unpremultiply(p[2], a), unpremultiply(p[4], a)
		})},
		"CMYK": {cmyk, want(func(k int) (uint8, uint8, uint8) {
			return cmyk.Pix[4*k], cmyk.Pix[4*k+1], cmyk.Pix[4*k+2]
		})},
		"Paletted": {paletted, want(func(k int) (uint8, uint8, uint8) {
			return paletted.Pix[k], paletted.Pix[k], paletted.Pix[k]
		})},
		"Gray16": {gray16, want(func(k int) (uint8, uint8, uint8) {
			y := uint8(min(gray16.Gray16At(k%w, k/w).Y, 255))
			return y, y, y
		})},
		"NYCbCrA": {nycbcra, lumaGeneric(t, &nycbcra.YCbCr)},
		"Gray":    {gray, lumaGeneric(t, gray)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := lumaHMA(t, tc.img)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("%s: luma[%d] = %v, want %v", name, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestHMACompatible_PaletteReorderingMovesOnlyTheHMAHash pins why rendered
// colour is the default: permuting a palette and remapping its indices leaves
// the image looking the same, and must leave the default hash unchanged. The
// HMA hash follows the indices, so the same re-encoding moves it.
func TestHMACompatible_PaletteReorderingMovesOnlyTheHMAHash(t *testing.T) {
	const w, h = 96, 64
	rng := rand.New(rand.NewSource(3))
	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255}
	}
	orig := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	for y := range h {
		for x := range w {
			orig.SetColorIndex(x, y, uint8((x/8+y/8)*37%256))
		}
	}

	perm := rng.Perm(256)
	permuted := make(color.Palette, 256)
	for i, p := range perm {
		permuted[p] = palette[i]
	}
	reordered := image.NewPaletted(orig.Rect, permuted)
	for i, k := range orig.Pix {
		reordered.Pix[i] = uint8(perm[k])
	}

	a, err := ComputeHash(orig)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeHash(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if a.Data != b.Data {
		t.Errorf("default hash changed under palette reordering: %s vs %s", a.Data, b.Data)
	}

	ha, err := ComputeHash(orig, HMACompatible())
	if err != nil {
		t.Fatal(err)
	}
	hb, err := ComputeHash(reordered, HMACompatible())
	if err != nil {
		t.Fatal(err)
	}
	d, err := HammingDistance(ha.Data, hb.Data)
	if err != nil {
		t.Fatal(err)
	}
	if d <= 31 {
		t.Errorf("HMA hash moved only %d bits under palette reordering; expected far beyond the match threshold", d)
	}
}
