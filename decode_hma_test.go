package pdq

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func decodeHMABytes(data []byte) (image.Image, error) {
	return decodeHMA(bytes.NewReader(data), int64(len(data)))
}

// tinyGIF is a 1x1 GIF with a two-colour palette whose one pixel, pixel (0
// or 1), sits at x, y on the given logical screen, after the given graphic
// control extensions.
func tinyGIF(screenW, screenH, x, y uint16, pixel byte, gces ...[]byte) []byte {
	b := []byte("GIF89a")
	b = binary.LittleEndian.AppendUint16(b, screenW)
	b = binary.LittleEndian.AppendUint16(b, screenH)
	b = append(b, 0x80, 0, 0) // global colour table of 2 entries
	b = append(b, 0, 0, 0, 255, 255, 255)
	for _, g := range gces {
		b = append(b, g...)
	}
	b = append(b, 0x2C)
	b = binary.LittleEndian.AppendUint16(b, x)
	b = binary.LittleEndian.AppendUint16(b, y)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = append(b, 0) // no local colour table
	// LZW, 3-bit codes: clear, the pixel, end.
	b = append(b, 2, 2, 0x44|pixel<<3, 1, 0)
	return append(b, 0x3B)
}

// gce is a graphic control extension, with the transparent flag set when
// transparent is not negative.
func gce(transparent int) []byte {
	if transparent < 0 {
		return []byte{0x21, 0xF9, 4, 0, 0, 0, 0, 0}
	}
	return []byte{0x21, 0xF9, 4, 1, 0, 0, byte(transparent), 0}
}

func TestDecodeHMA_GIFScreenOverLimit(t *testing.T) {
	cases := map[string][]byte{
		"screen":        tinyGIF(65535, 65535, 0, 0, 0),
		"frame overrun": tinyGIF(1, 1, 65534, 65534, 0),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := decodeHMABytes(data)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, errTooLarge) {
				t.Fatalf("decodeHMA: got %v, want errTooLarge", err)
			}
			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
				t.Errorf("allocated %d bytes before rejecting the screen", alloc)
			}
		})
	}
}

// TestDecodeHMA_GIFFramePlacement checks the frame lands at its offset and
// the rest of the screen holds the transparent index, which a later GCE
// without one does not clear and which may be past the palette's end.
func TestDecodeHMA_GIFFramePlacement(t *testing.T) {
	img, err := decodeHMABytes(tinyGIF(4, 3, 2, 1, 1, gce(3), gce(-1)))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := img.(*image.Paletted)
	if !ok {
		t.Fatalf("decoded %T, want *image.Paletted", img)
	}
	if p.Rect != image.Rect(0, 0, 4, 3) {
		t.Fatalf("bounds %v, want the 4x3 screen", p.Rect)
	}
	for y := range 3 {
		for x := range 4 {
			want := uint8(3)
			if x == 2 && y == 1 {
				want = 1
			}
			if got := p.ColorIndexAt(x, y); got != want {
				t.Errorf("index at (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}

// TestDecodeHMA_GIFScreenGrownPast16Bits checks a frame ending past 65535,
// which Pillow grows the screen to hold.
func TestDecodeHMA_GIFScreenGrownPast16Bits(t *testing.T) {
	img, err := decodeHMABytes(tinyGIF(1, 1, 65535, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := img.(*image.Paletted)
	if !ok {
		t.Fatalf("decoded %T, want *image.Paletted", img)
	}
	if p.Rect != image.Rect(0, 0, 65536, 1) {
		t.Fatalf("bounds %v, want 65536x1", p.Rect)
	}
	if p.ColorIndexAt(65535, 0) != 1 || p.ColorIndexAt(0, 0) != 0 {
		t.Error("frame not placed at x=65535 on a 0-filled screen")
	}
}

type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// tiffTypeWidth is the byte width of each integer field type.
var tiffTypeWidth = map[uint16]int{1: 1, 6: 1, 3: 2, 8: 2, 4: 4, 9: 4}

// tiffIFD is a TIFF header and one IFD of a 1-bit image with the given
// Compression and Photometric. Those and SamplesPerPixel are stored with the
// given field type, each value repeated count times, after the IFD when that
// does not fit in the entry.
func tiffIFD(order byteOrder, typ uint16, count, compression, photometric uint32) []byte {
	entries := []struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}{{258, 3, 1, 1}, {259, typ, count, compression}, {262, typ, count, photometric}, {277, typ, count, 1}}
	encode := func(e struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}) []byte {
		var v []byte
		for range e.count {
			switch tiffTypeWidth[e.typ] {
			case 1:
				v = append(v, byte(e.value))
			case 2:
				v = order.AppendUint16(v, uint16(e.value))
			case 4:
				v = order.AppendUint32(v, e.value)
			}
		}
		return v
	}
	b := []byte("II*\x00")
	if order == binary.BigEndian {
		b = []byte("MM\x00*")
	}
	b = order.AppendUint32(b, 8)
	b = order.AppendUint16(b, uint16(len(entries)))
	extra := uint32(8 + 2 + 12*len(entries) + 4)
	var tail []byte
	for _, e := range entries {
		b = order.AppendUint16(b, e.tag)
		b = order.AppendUint16(b, e.typ)
		b = order.AppendUint32(b, e.count)
		v := encode(e)
		if len(v) > 4 {
			b = order.AppendUint32(b, extra+uint32(len(tail)))
			tail = append(tail, v...)
			continue
		}
		b = append(b, append(v, make([]byte, 4-len(v))...)...)
	}
	b = order.AppendUint32(b, 0)
	return append(b, tail...)
}

func TestTIFFBilevel_FieldTypes(t *testing.T) {
	for _, order := range []byteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, typ := range []uint16{1, 3, 4, 6, 8, 9} {
			for _, count := range []uint32{1, 3} {
				for _, tc := range []struct {
					compression, photometric uint32
					inverted                 bool
				}{{4, 1, true}, {3, 1, true}, {4, 0, false}, {1, 1, false}} {
					data := tiffIFD(order, typ, count, tc.compression, tc.photometric)
					isBilevel, inverted, err := tiffBilevel(bytes.NewReader(data), int64(len(data)))
					if err != nil {
						t.Fatal(err)
					}
					if !isBilevel || inverted != tc.inverted {
						t.Errorf("%v type %d count %d compression %d photometric %d: bilevel %v inverted %v, want true %v",
							order, typ, count, tc.compression, tc.photometric, isBilevel, inverted, tc.inverted)
					}
				}
			}
		}
	}
}

func TestDecodeHMA_GIFEmptyFrame(t *testing.T) {
	data := tinyGIF(4, 4, 1, 1, 0)
	desc := bytes.IndexByte(data[19:], 0x2C) + 19
	binary.LittleEndian.PutUint16(data[desc+5:], 0) // width 0
	// LZW with no pixels, clear then end, so image/gif accepts the frame.
	data = append(data[:desc+10], 2, 1, 0x2C, 0, 0x3B)
	if _, err := decodeHMABytes(data); err == nil {
		t.Error("decoded a GIF whose first frame is empty; Pillow refuses it")
	}
}

// TestComputeHashFromFile_HMAStreams checks that HMA mode does not read a
// whole file into memory: each image is followed by 256 MiB of sparse
// padding that no decoder reaches.
func TestComputeHashFromFile_HMAStreams(t *testing.T) {
	const padded = 256 << 20
	for _, name := range []string{"photo-gif-offset-frame.gif", "photo-png-rgb.png", "photo-tiff-1bit-g4.tiff"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "hma", name))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, padded); err != nil {
				t.Fatal(err)
			}
			want, err := ComputeHashFromBytes(data, HMACompatible())
			if err != nil {
				t.Fatal(err)
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			got, err := ComputeHashFromFile(path, HMACompatible())
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if *got != *want {
				t.Errorf("padded file hashes to %v, the image alone to %v", got, want)
			}
			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 32<<20 {
				t.Errorf("allocated %d MiB hashing a %d MiB file", alloc>>20, padded>>20)
			}
		})
	}
}
