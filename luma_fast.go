package pdq

import (
	"image"
	"image/color"
)

// fillFloatLumaFast fills luma for the common concrete image types without the
// per-pixel image.Image.At / color.Color interface dispatch. It returns false
// for any type it does not specialize, leaving the caller to use the generic
// path.
//
// Each branch is bit-identical to fillFloatLumaGeneric: the *image.RGBA and
// *image.Gray paths exploit that At().RGBA()>>8 equals the stored 8-bit sample,
// and the *image.YCbCr and *image.NRGBA paths invoke the exact same stdlib
// color conversion (color.YCbCr.RGBA / color.NRGBA.RGBA) that At() would,
// reached through the concrete plane/Pix offsets. TestFillFloatLuma_FastMatchesGeneric
// pins this equality.
func fillFloatLumaFast(img image.Image, luma []float32, numRows, numCols int) bool {
	b := img.Bounds()
	switch src := img.(type) {
	case *image.RGBA:
		for i := range numRows {
			o := src.PixOffset(b.Min.X, b.Min.Y+i)
			for j := range numCols {
				p := o + j*4
				luma[i*numCols+j] = lumaFromRGB8(src.Pix[p], src.Pix[p+1], src.Pix[p+2])
			}
		}
		return true
	case *image.Gray:
		for i := range numRows {
			o := src.PixOffset(b.Min.X, b.Min.Y+i)
			for j := range numCols {
				y := src.Pix[o+j]
				luma[i*numCols+j] = lumaFromRGB8(y, y, y)
			}
		}
		return true
	case *image.NRGBA:
		for i := range numRows {
			o := src.PixOffset(b.Min.X, b.Min.Y+i)
			for j := range numCols {
				p := o + j*4
				r, g, bl, _ := color.NRGBA{R: src.Pix[p], G: src.Pix[p+1], B: src.Pix[p+2], A: src.Pix[p+3]}.RGBA()
				luma[i*numCols+j] = lumaFromRGB8(uint8(r>>8), uint8(g>>8), uint8(bl>>8))
			}
		}
		return true
	case *image.YCbCr:
		for i := range numRows {
			y := b.Min.Y + i
			for j := range numCols {
				x := b.Min.X + j
				yi := src.YOffset(x, y)
				ci := src.COffset(x, y)
				r, g, bl, _ := color.YCbCr{Y: src.Y[yi], Cb: src.Cb[ci], Cr: src.Cr[ci]}.RGBA()
				luma[i*numCols+j] = lumaFromRGB8(uint8(r>>8), uint8(g>>8), uint8(bl>>8))
			}
		}
		return true
	default:
		return false
	}
}
