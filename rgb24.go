package pdq

import (
	"fmt"
	"sync"
)

// floatPool recycles ComputeHashFromRGB24's two frame-sized buffers, which a
// video hashes once per sampled frame.
var floatPool sync.Pool

func getFloats(n int) *[]float32 {
	if p, ok := floatPool.Get().(*[]float32); ok && cap(*p) >= n {
		*p = (*p)[:n]
		return p
	}
	s := make([]float32, n)
	return &s
}

// RGB24Option configures ComputeHashFromRGB24.
type RGB24Option func(*rgb24Options)

type rgb24Options struct {
	cppLuma bool
}

// CPPLuma computes luma in float32 throughout, as the ThreatExchange C++ and
// so vPDQ do. Without it, luma is pdqhash-python's, as for images.
func CPPLuma() RGB24Option {
	return func(o *rgb24Options) { o.cppLuma = true }
}

// ComputeHashFromRGB24 hashes an interleaved 8-bit RGB buffer of
// numRows x numCols pixels, as an ffmpeg rgb24 frame arrives.
func ComputeHashFromRGB24(rgb []byte, numRows, numCols int, opts ...RGB24Option) (*Hash, error) {
	if numRows < MinHashableDim || numCols < MinHashableDim {
		return nil, ErrImageTooSmall
	}
	n := numRows * numCols
	if len(rgb) != 3*n {
		return nil, fmt.Errorf("rgb24 length mismatch: expected %d, got %d", 3*n, len(rgb))
	}

	lumaBuf, scratch := getFloats(n), getFloats(n)
	defer floatPool.Put(lumaBuf)
	defer floatPool.Put(scratch)
	var o rgb24Options
	for _, opt := range opts {
		opt(&o)
	}
	fillLumaFromRGB24(rgb, *lumaBuf, o.cppLuma)

	hashBytes, quality := hashLumaInPlace(*lumaBuf, *scratch, numRows, numCols)
	return &Hash{Data: formatHash(hashBytes), Quality: quality}, nil
}

// fillLumaFromRGB24 converts each interleaved RGB pixel to luma, with the C++
// arithmetic when cpp is set.
func fillLumaFromRGB24(rgb []byte, luma []float32, cpp bool) {
	if cpp {
		for k := range luma {
			luma[k] = lumaFromRGB8Float32(rgb[3*k], rgb[3*k+1], rgb[3*k+2])
		}
		return
	}
	for k := range luma {
		luma[k] = lumaFromRGB8(rgb[3*k], rgb[3*k+1], rgb[3*k+2])
	}
}

// lumaFromRGB8Float32 computes luma with float32 coefficients, products and
// sums, left to right. Each explicit conversion rounds, which keeps the
// compiler from fusing a multiply into the add.
func lumaFromRGB8Float32(r, g, b uint8) float32 {
	const cr, cg, cb float32 = 0.299, 0.587, 0.114
	return float32(float32(cr*float32(r))+float32(cg*float32(g))) + float32(cb*float32(b))
}
