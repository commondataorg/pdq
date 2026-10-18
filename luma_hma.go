package pdq

import "image"

// fillFloatLumaHMA fills luma from the samples HMACompatible hashes: colour
// channels with alpha ignored (premultiplied types are first
// unpremultiplied), a palette image's indices, CMYK channels read as RGB,
// 16-bit grey clipped at 255 and 16-bit colour's high byte. Types it does
// not handle fall back to the default path.
func fillFloatLumaHMA(img image.Image, luma []float32, numRows, numCols int) {
	b := img.Bounds()
	switch src := img.(type) {
	case *image.RGBA:
		fillUnpremultiplied(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 4, 1, luma, numRows, numCols)
	case *image.RGBA64:
		fillUnpremultiplied(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 8, 2, luma, numRows, numCols)
	case *image.NRGBA:
		fillFromPix(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 4, 1, luma, numRows, numCols)
	case *image.NRGBA64:
		fillFromPix(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 8, 2, luma, numRows, numCols)
	case *image.CMYK:
		fillFromPix(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 4, 1, luma, numRows, numCols)
	case *image.Paletted:
		fillFromPix(src.Pix, src.Stride, src.PixOffset(b.Min.X, b.Min.Y), 1, 0, luma, numRows, numCols)
	case *image.Gray16:
		for i := range numRows {
			o := src.PixOffset(b.Min.X, b.Min.Y+i)
			for j := range numCols {
				p := o + j*2
				y := uint8(min(uint16(src.Pix[p])<<8|uint16(src.Pix[p+1]), 255))
				luma[i*numCols+j] = lumaFromRGB8(y, y, y)
			}
		}
	case *image.NYCbCrA:
		fillFloatLumaFromImage(&src.YCbCr, luma, numRows, numCols)
	default:
		// Gray and YCbCr hash the same samples either way.
		fillFloatLumaFromImage(img, luma, numRows, numCols)
	}
}

// fillFromPix reads the first three channels of each pixel from their high
// bytes. chanBytes 0 reads one sample as all three channels.
func fillFromPix(pix []uint8, stride, origin, pixBytes, chanBytes int, luma []float32, numRows, numCols int) {
	for i := range numRows {
		o := origin + i*stride
		for j := range numCols {
			p := o + j*pixBytes
			luma[i*numCols+j] = lumaFromRGB8(pix[p], pix[p+chanBytes], pix[p+2*chanBytes])
		}
	}
}

// fillUnpremultiplied reads premultiplied pixels as straight colour from
// their high bytes: zero where alpha is 0, and otherwise each channel times
// 255 over alpha, truncated and clipped at 255.
func fillUnpremultiplied(pix []uint8, stride, origin, pixBytes, chanBytes int, luma []float32, numRows, numCols int) {
	unmul := func(v, a uint8) uint8 {
		return uint8(min(int(v)*255/int(a), 255))
	}
	for i := range numRows {
		o := origin + i*stride
		for j := range numCols {
			p := o + j*pixBytes
			r, g, bl, a := pix[p], pix[p+chanBytes], pix[p+2*chanBytes], pix[p+3*chanBytes]
			switch a {
			case 0:
				r, g, bl = 0, 0, 0
			case 255:
			default:
				r, g, bl = unmul(r, a), unmul(g, a), unmul(bl, a)
			}
			luma[i*numCols+j] = lumaFromRGB8(r, g, bl)
		}
	}
}
