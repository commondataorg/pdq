package pdq

import "image"

// Option configures how an image's pixels become luma.
type Option func(*options)

type options struct {
	hma bool
}

// HMACompatible hashes an image's encoded samples rather than its rendered
// colour, to string-match corpora built with HMA; the README lists the rules.
// Do not use it for enforcement matching: re-encoding an image without
// changing how it looks can move this hash far from the original.
func HMACompatible() Option {
	return func(o *options) { o.hma = true }
}

func applyOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// fillLuma converts img to luma: rendered colour by default, encoded samples
// with HMACompatible.
func fillLuma(img image.Image, luma []float32, numRows, numCols int, o options) {
	if o.hma {
		fillFloatLumaHMA(img, luma, numRows, numCols)
		return
	}
	fillFloatLumaFromImage(img, luma, numRows, numCols)
}
