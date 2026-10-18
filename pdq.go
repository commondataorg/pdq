package pdq

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"unsafe"

	_ "golang.org/x/image/webp"
)

const (
	// HashSize is the size of the PDQ hash in hex characters (64 hex chars = 256 bits)
	HashSize = 64
	// MinHashableDim is the minimum image dimension required for hashing
	MinHashableDim = 5
	// numJaroszPasses is the number of Jarosz filter passes (tent filter)
	numJaroszPasses = 2
)

var (
	// ErrImageTooSmall is returned when the image is too small to hash
	ErrImageTooSmall = errors.New("image too small to hash (minimum 5x5)")
	// ErrInvalidImage is returned when the image cannot be decoded
	ErrInvalidImage = errors.New("invalid or unsupported image format")
)

// Hash represents a PDQ hash result
type Hash struct {
	// Data is the 64-character hexadecimal hash string
	Data string
	// Quality is the hash quality score (0-100)
	Quality int
}

// ComputeHash computes PDQ hash from an image.Image
func ComputeHash(img image.Image, opts ...Option) (*Hash, error) {
	bounds := img.Bounds()
	numRows := bounds.Dy()
	numCols := bounds.Dx()

	if numRows < MinHashableDim || numCols < MinHashableDim {
		return nil, ErrImageTooSmall
	}

	// Convert image to luminance
	lumaBuffer := make([]float32, numRows*numCols)
	fillLuma(img, lumaBuffer, numRows, numCols, applyOptions(opts))

	hashBytes, quality := computeHashFromLuma(lumaBuffer, numRows, numCols)

	return &Hash{
		Data:    formatHash(hashBytes),
		Quality: quality,
	}, nil
}

// ComputeHashFromFile computes PDQ hash from an image file
func ComputeHashFromFile(path string, opts ...Option) (*Hash, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var img image.Image
	if applyOptions(opts).hma {
		st, serr := f.Stat()
		if serr != nil {
			return nil, fmt.Errorf("failed to stat file: %w", serr)
		}
		img, err = decodeHMA(f, st.Size())
	} else {
		img, _, err = image.Decode(f)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	return ComputeHash(img, opts...)
}

// ComputeHashFromBytes computes PDQ hash from image bytes
func ComputeHashFromBytes(data []byte, opts ...Option) (*Hash, error) {
	var img image.Image
	var err error
	if applyOptions(opts).hma {
		img, err = decodeHMA(bytes.NewReader(data), int64(len(data)))
	} else {
		img, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	return ComputeHash(img, opts...)
}

// ComputeHashBase64 returns base64-encoded hash and quality score
func ComputeHashBase64(img image.Image, opts ...Option) (string, int, error) {
	bounds := img.Bounds()
	numRows := bounds.Dy()
	numCols := bounds.Dx()

	if numRows < MinHashableDim || numCols < MinHashableDim {
		return "", 0, ErrImageTooSmall
	}

	// Convert image to luminance
	lumaBuffer := make([]float32, numRows*numCols)
	fillLuma(img, lumaBuffer, numRows, numCols, applyOptions(opts))

	hashBytes, quality := computeHashFromLuma(lumaBuffer, numRows, numCols)

	return base64.StdEncoding.EncodeToString(hashBytes[:]), quality, nil
}

// ComputeHashFromRGBBytes computes PDQ hash from raw uint8 RGB channel arrays.
// Luminance is computed using float64 intermediates then truncated to float32,
// matching pdqhash-python's numpy-based luminance conversion:
//
//	gray = (R*0.299 + G*0.587 + B*0.114).astype('float32')
func ComputeHashFromRGBBytes(rr, gg, bb []uint8, numRows, numCols int) (*Hash, error) {
	if numRows < MinHashableDim || numCols < MinHashableDim {
		return nil, ErrImageTooSmall
	}
	n := numRows * numCols
	if len(rr) != n || len(gg) != n || len(bb) != n {
		return nil, fmt.Errorf("channel length mismatch: expected %d, got r=%d g=%d b=%d", n, len(rr), len(gg), len(bb))
	}

	lumaBuffer := make([]float32, n)
	fillFloatLumaFromRGB(rr, gg, bb, lumaBuffer, n)

	hashBytes, quality := computeHashFromLuma(lumaBuffer, numRows, numCols)
	return &Hash{
		Data:    formatHash(hashBytes),
		Quality: quality,
	}, nil
}

// fillFloatLumaFromRGB converts raw uint8 RGB channels to luminance.
// Uses float64 intermediates then truncates to float32, matching pdqhash-python's
// numpy luminance: (uint8*0.299 + uint8*0.587 + uint8*0.114).astype('float32').
func fillFloatLumaFromRGB(rr, gg, bb []uint8, luma []float32, n int) {
	for k := range n {
		luma[k] = float32(float64(rr[k])*0.299 +
			float64(gg[k])*0.587 +
			float64(bb[k])*0.114)
	}
}

// fillFloatLumaFromImage converts an image to luminance values.
// Uses the same float64 intermediate arithmetic as fillFloatLumaFromRGB so that
// ComputeHash(img) and ComputeHashFromRGBBytes(extractRGB(img)) produce identical
// hashes for the same pixel data.
//
// For the common concrete image types it dispatches to a fast path that avoids
// the per-pixel image.Image/color.Color interface calls; the fast paths are
// bit-exact with the generic path because they invoke the same stdlib pixel
// math (see luma_fast.go). Any unrecognized type falls back to the generic
// At()-based loop.
func fillFloatLumaFromImage(img image.Image, luma []float32, numRows, numCols int) {
	if fillFloatLumaFast(img, luma, numRows, numCols) {
		return
	}
	fillFloatLumaGeneric(img, luma, numRows, numCols)
}

// fillFloatLumaGeneric is the reference luminance conversion using the
// image.Image.At interface. It is the fallback for image types without a fast
// path and the oracle the fast paths are validated against.
func fillFloatLumaGeneric(img image.Image, luma []float32, numRows, numCols int) {
	bounds := img.Bounds()
	for i := range numRows {
		for j := range numCols {
			r, g, b, _ := img.At(bounds.Min.X+j, bounds.Min.Y+i).RGBA()
			// RGBA returns values in [0, 65535]; right-shift to uint8 [0, 255]
			// then compute luminance with float64 intermediates matching
			// pdqhash-python's numpy arithmetic.
			luma[i*numCols+j] = lumaFromRGB8(uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}
}

// lumaFromRGB8 computes the PDQ luminance of one 8-bit RGB pixel using the exact
// float64 intermediate arithmetic shared by every code path (generic and fast),
// so all paths are bit-identical.
func lumaFromRGB8(r, g, b uint8) float32 {
	return float32(
		float64(r)*0.299 +
			float64(g)*0.587 +
			float64(b)*0.114)
}

// computeHashFromLuma computes the PDQ hash from luminance data.
func computeHashFromLuma(lumaBuffer []float32, numRows, numCols int) ([32]byte, int) {
	// Create working buffers
	buffer1 := make([]float32, numRows*numCols)
	copy(buffer1, lumaBuffer)
	buffer2 := make([]float32, numRows*numCols)
	return hashLumaInPlace(buffer1, buffer2, numRows, numCols)
}

// hashLumaInPlace hashes the luma in buffer1, overwriting it; buffer2 is
// scratch of the same length.
func hashLumaInPlace(buffer1, buffer2 []float32, numRows, numCols int) ([32]byte, int) {
	var buffer64x64 [64][64]float32
	var buffer16x64 [16][64]float32
	var buffer16x16 [16][16]float32

	if numRows == 64 && numCols == 64 {
		// Already the right size, just copy
		for i := range 64 {
			for j := range 64 {
				buffer64x64[i][j] = buffer1[i*64+j]
			}
		}
	} else {
		// Downsample (blur and decimate)
		windowSizeAlongRows := computeJaroszFilterWindowSize(numCols, 64)
		windowSizeAlongCols := computeJaroszFilterWindowSize(numRows, 64)

		jaroszFilterFloat(buffer1, buffer2, numRows, numCols,
			windowSizeAlongRows, windowSizeAlongCols, numJaroszPasses)

		// Decimate to 64x64
		decimateFloat(buffer1, numRows, numCols, buffer64x64Flat(&buffer64x64), 64, 64)
	}

	// Quality metric
	quality := pdqImageDomainQualityMetric(&buffer64x64)

	// 2D DCT
	dct64To16(&buffer64x64, &buffer16x64, &buffer16x16)

	// Convert to hash bits
	hashBytes := pdqBuffer16x16ToBits(&buffer16x16)

	return hashBytes, quality
}

// buffer64x64Flat returns a flat slice view of the 64x64 buffer.
// This is safe because Go arrays are contiguous in memory, so a [64][64]float32
// has the same memory layout as a [4096]float32.
func buffer64x64Flat(b *[64][64]float32) []float32 {
	return unsafe.Slice(&b[0][0], 64*64)
}

// pdqBuffer16x16ToBits converts the 16x16 DCT output to hash bits.
// Each bit is 1 if the corresponding DCT value is greater than the median.
func pdqBuffer16x16ToBits(dctOutput *[16][16]float32) [32]byte {
	// Flatten for median calculation
	flat := make([]float32, 256)
	for i := range 16 {
		for j := range 16 {
			flat[i*16+j] = dctOutput[i][j]
		}
	}

	dctMedian := torbenMedian(flat)

	// Build hash using the same bit layout as the C++ implementation
	// The C++ uses 16 x 16-bit words, with bit k stored as:
	// words[(k & 255) >> 4] |= 1 << (k & 15)
	var words [16]uint16
	for i := range 16 {
		for j := range 16 {
			k := i*16 + j
			if dctOutput[i][j] > dctMedian {
				words[(k&255)>>4] |= 1 << (k & 15)
			}
		}
	}

	// Convert words to bytes
	// The C++ format() function prints words[15] down to words[0]
	// Each word is printed as %04hx (4 hex chars, little-endian interpretation)
	var hashBytes [32]byte
	for i := range 16 {
		// Words are stored with word[15] first in the hex string
		// Each uint16 is stored as two bytes in little-endian within the word
		hashBytes[i*2] = byte(words[15-i] >> 8)
		hashBytes[i*2+1] = byte(words[15-i] & 0xff)
	}

	return hashBytes
}

// formatHash converts the 32-byte hash to a 64-character hex string
func formatHash(hashBytes [32]byte) string {
	return hex.EncodeToString(hashBytes[:])
}

// HammingDistance computes the Hamming distance between two PDQ hashes
func HammingDistance(hash1, hash2 string) (int, error) {
	if len(hash1) != HashSize || len(hash2) != HashSize {
		return 0, fmt.Errorf("invalid hash length: expected %d, got %d and %d", HashSize, len(hash1), len(hash2))
	}

	bytes1, err := hex.DecodeString(hash1)
	if err != nil {
		return 0, fmt.Errorf("invalid hash1: %w", err)
	}

	bytes2, err := hex.DecodeString(hash2)
	if err != nil {
		return 0, fmt.Errorf("invalid hash2: %w", err)
	}

	distance := 0
	for i := range bytes1 {
		xor := bytes1[i] ^ bytes2[i]
		for xor != 0 {
			distance += int(xor & 1)
			xor >>= 1
		}
	}

	return distance, nil
}
