package pdq

import (
	"encoding/base64"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	"os"
	"slices"
	"testing"
)

// testImage is a 1600x1004 JPEG (ThreatExchange's bridge-mods/aaa-orig.jpg).
const testImage = "testdata/bridge.jpg"

func TestComputeHashFromFile(t *testing.T) {
	hash, err := ComputeHashFromFile(testImage)
	if err != nil {
		t.Fatalf("Failed to compute PDQ hash: %v", err)
	}

	if hash == nil {
		t.Fatal("Expected non-nil hash")
	}

	if len(hash.Data) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash.Data))
	}

	if hash.Quality < 0 || hash.Quality > 100 {
		t.Errorf("Expected quality between 0-100, got %d", hash.Quality)
	}

	t.Logf("PDQ Hash: %s (quality: %d)", hash.Data, hash.Quality)

	// Compare with the pdqhash reference hash (Meta's PDQ C++ via Python
	// bindings, threatexchange 1.2.16) computed on the same file.
	refHash := "f8f8f0cee0f4a84f06370a22038f63f0b36e2ed596621e1d33e6b39c4e9c9b22"

	dist, err := HammingDistance(hash.Data, refHash)
	if err != nil {
		t.Fatalf("Failed to compute hamming distance: %v", err)
	}

	t.Logf("Hamming distance from pdqhash reference: %d bits", dist)

	// The pure Go implementation may differ slightly from pdqhash due to
	// different JPEG decoding libraries (Go's image/jpeg vs Pillow).
	// A hamming distance of <= 10 is acceptable as both implementations
	// will identify the same similar images (PDQ threshold is typically 31).
	if dist > 10 {
		t.Errorf("Hash differs too much from pdqhash reference: %d bits (max 10)", dist)
	}
}

func TestComputeHashInvalidFile(t *testing.T) {
	_, err := ComputeHashFromFile("/nonexistent/file.jpg")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestHammingDistance(t *testing.T) {
	// Same hash should have distance 0
	hash := "0000000000000000000000000000000000000000000000000000000000000000"
	dist, err := HammingDistance(hash, hash)
	if err != nil {
		t.Fatalf("HammingDistance failed: %v", err)
	}
	if dist != 0 {
		t.Errorf("Expected distance 0, got %d", dist)
	}

	// All zeros vs all ones should have distance 256
	allZeros := "0000000000000000000000000000000000000000000000000000000000000000"
	allOnes := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	dist, err = HammingDistance(allZeros, allOnes)
	if err != nil {
		t.Fatalf("HammingDistance failed: %v", err)
	}
	if dist != 256 {
		t.Errorf("Expected distance 256, got %d", dist)
	}

	// Single bit difference
	hash1 := "0000000000000000000000000000000000000000000000000000000000000000"
	hash2 := "0000000000000000000000000000000000000000000000000000000000000001"
	dist, err = HammingDistance(hash1, hash2)
	if err != nil {
		t.Fatalf("HammingDistance failed: %v", err)
	}
	if dist != 1 {
		t.Errorf("Expected distance 1, got %d", dist)
	}
}

func TestHammingDistanceInvalid(t *testing.T) {
	// Wrong length
	_, err := HammingDistance("abc", "def")
	if err == nil {
		t.Error("Expected error for invalid hash length")
	}

	// Invalid hex
	_, err = HammingDistance(
		"000000000000000000000000000000000000000000000000000000000000000g",
		"0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err == nil {
		t.Error("Expected error for invalid hex")
	}
}

func TestComputeHashBase64(t *testing.T) {
	f, err := os.Open(testImage)
	if err != nil {
		t.Fatalf("Failed to open test image: %v", err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := decodeImage(f)
	if err != nil {
		t.Fatalf("Failed to decode image: %v", err)
	}

	b64Hash, quality, err := ComputeHashBase64(img)
	if err != nil {
		t.Fatalf("ComputeHashBase64 failed: %v", err)
	}

	// Verify base64 decodes to 32 bytes
	decoded, err := base64.StdEncoding.DecodeString(b64Hash)
	if err != nil {
		t.Fatalf("Failed to decode base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("Expected 32 bytes, got %d", len(decoded))
	}

	// Convert to hex and compare with hex version
	hexHash := hex.EncodeToString(decoded)

	hash, err := ComputeHash(img)
	if err != nil {
		t.Fatalf("ComputeHash failed: %v", err)
	}

	if hexHash != hash.Data {
		t.Errorf("Base64 and hex hashes don't match:\n  base64->hex: %s\n  direct hex:  %s", hexHash, hash.Data)
	}

	if quality != hash.Quality {
		t.Errorf("Quality mismatch: base64=%d, hex=%d", quality, hash.Quality)
	}

	t.Logf("Base64 hash: %s (quality: %d)", b64Hash, quality)
}

func TestTorbenMedian(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected float32
	}{
		{
			name:     "single element",
			input:    []float32{5},
			expected: 5,
		},
		{
			name:     "two elements",
			input:    []float32{1, 3},
			expected: 1,
		},
		{
			name:     "three elements",
			input:    []float32{1, 2, 3},
			expected: 2,
		},
		{
			name:     "four elements",
			input:    []float32{1, 2, 3, 4},
			expected: 2,
		},
		{
			name:     "with duplicates",
			input:    []float32{1, 2, 2, 3},
			expected: 2,
		},
		{
			name:     "all same",
			input:    []float32{5, 5, 5, 5},
			expected: 5,
		},
		{
			name:     "unsorted",
			input:    []float32{5, 1, 3, 2, 4},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := torbenMedian(tt.input)
			if result != tt.expected {
				t.Errorf("torbenMedian() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTorbenMedianLargeArray(t *testing.T) {
	// Test with 256 elements (same as PDQ)
	input := make([]float32, 256)
	for i := range input {
		input[i] = float32(i)
	}

	result := torbenMedian(input)
	// For 256 elements, the lower median should be at index 127
	expected := float32(127)

	if result != expected {
		t.Errorf("torbenMedian() = %v, want %v", result, expected)
	}
}

func TestTorbenVsSort(t *testing.T) {
	input := make([]float32, 256)
	for i := range input {
		input[i] = float32(i*7%256) - 128
	}

	torbenResult := torbenMedian(input)

	sorted := make([]float32, len(input))
	copy(sorted, input)
	slices.Sort(sorted)

	n := len(sorted)
	lowerMiddle := sorted[(n-1)/2]
	upperMiddle := sorted[n/2]

	if torbenResult < lowerMiddle || torbenResult > upperMiddle {
		t.Errorf("torben result %f not in median range [%f, %f]", torbenResult, lowerMiddle, upperMiddle)
	}
}

func BenchmarkComputeHash(b *testing.B) {
	for b.Loop() {
		_, err := ComputeHashFromFile(testImage)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTorbenMedian(b *testing.B) {
	input := make([]float32, 256)
	for i := range input {
		input[i] = float32(i*7%256) - 128
	}

	b.ResetTimer()
	for b.Loop() {
		torbenMedian(input)
	}
}

// decodeImage is a helper for tests
func decodeImage(f *os.File) (image.Image, string, error) {
	return image.Decode(f)
}
