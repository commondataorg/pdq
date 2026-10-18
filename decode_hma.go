package pdq

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"io"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// maxHMAPixels is the size above which Pillow raises DecompressionBombError,
// so HMA hashes nothing: twice its MAX_IMAGE_PIXELS.
const maxHMAPixels = 2 * (1024 * 1024 * 1024 / 4 / 3)

var (
	pngSignature = []byte("\x89PNG\r\n\x1a\n")
	errTooLarge  = errors.New("image is over the pixel limit")
)

// decodeHMA decodes the size bytes in r into the image HMACompatible hashes,
// applying what the decoded image.Image cannot carry:
//   - a GIF's first frame sits on its logical screen, grown to fit the frame
//     and filled outside it with the frame's transparent index, or 0;
//   - a 16-bit greyscale PNG stays 16-bit grey even with a tRNS chunk;
//   - a 1-bit image, PNG or TIFF, holds 0 and 1 rather than 0 and 255.
//
// BMP and TIFF are decoded here rather than registered with image.Decode for
// every importer. The input is streamed, never read whole, and every format is
// checked against maxHMAPixels before its pixels are allocated.
func decodeHMA(r io.ReaderAt, size int64) (image.Image, error) {
	head := make([]byte, 26)
	n, err := r.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	section := func() io.Reader { return io.NewSectionReader(r, 0, size) }
	switch {
	case bytes.HasPrefix(head, []byte("GIF8")):
		return decodeGIFScreen(r, size)
	case bytes.HasPrefix(head, []byte("BM")):
		return decodeBounded(section, bmp.DecodeConfig, bmp.Decode)
	case bytes.HasPrefix(head, []byte("II*\x00")), bytes.HasPrefix(head, []byte("MM\x00*")):
		img, err := decodeBounded(section, tiff.DecodeConfig, tiff.Decode)
		if err != nil {
			return nil, err
		}
		g, ok := img.(*image.Gray)
		if !ok {
			return img, nil
		}
		isBilevel, inverted, err := tiffBilevel(r, size)
		if err != nil {
			return nil, err
		}
		if !isBilevel {
			return img, nil
		}
		b := bilevel(g)
		if inverted {
			for i := range b.Pix {
				b.Pix[i] ^= 1
			}
		}
		return b, nil
	}
	img, err := decodeBounded(section,
		func(r io.Reader) (image.Config, error) { c, _, err := image.DecodeConfig(r); return c, err },
		func(r io.Reader) (image.Image, error) { m, _, err := image.Decode(r); return m, err })
	if err != nil {
		return nil, err
	}
	// IHDR follows the signature: length, type, width, height, bit depth,
	// colour type.
	if bytes.HasPrefix(head, pngSignature) && len(head) >= 26 && head[25] == 0 {
		switch head[24] {
		case 16:
			if src, ok := img.(*image.NRGBA64); ok {
				return greyFromNRGBA64(src), nil
			}
		case 1:
			switch src := img.(type) {
			case *image.Gray:
				return bilevel(src), nil
			case *image.NRGBA:
				return bilevelFromNRGBA(src), nil
			}
		}
	}
	return img, nil
}

func decodeBounded(section func() io.Reader, config func(io.Reader) (image.Config, error), decode func(io.Reader) (image.Image, error)) (image.Image, error) {
	cfg, err := config(section())
	if err != nil {
		return nil, err
	}
	if err := checkPixels(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	return decode(section())
}

func checkPixels(w, h int) error {
	if int64(w)*int64(h) > maxHMAPixels {
		return fmt.Errorf("%w: %dx%d, limit %d pixels", errTooLarge, w, h, maxHMAPixels)
	}
	return nil
}

// gifFirstFrame is what decodeGIFScreen needs from a GIF's header and first
// image descriptor.
type gifFirstFrame struct {
	header           [13]byte // signature and logical screen descriptor
	descriptor       [10]byte // the first image descriptor
	offset           int64    // where descriptor starts
	screenW, screenH int
	frame            image.Rectangle
	transparent      int // -1 when the frame has none
}

// parseGIFFirstFrame reads the logical screen, then walks the extension
// blocks to the first image descriptor. Like Pillow and image/gif, it keeps
// the last transparent index any graphic control extension before it sets.
func parseGIFFirstFrame(r io.Reader) (gifFirstFrame, error) {
	br := bufio.NewReader(r)
	f := gifFirstFrame{transparent: -1}
	if _, err := io.ReadFull(br, f.header[:]); err != nil {
		return f, fmt.Errorf("gif: header: %w", err)
	}
	f.screenW = int(binary.LittleEndian.Uint16(f.header[6:]))
	f.screenH = int(binary.LittleEndian.Uint16(f.header[8:]))
	pos := int64(len(f.header))
	if flags := f.header[10]; flags&0x80 != 0 {
		n, err := br.Discard(3 << ((flags & 7) + 1))
		if err != nil {
			return f, fmt.Errorf("gif: colour table: %w", err)
		}
		pos += int64(n)
	}
	var block [255]byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return f, fmt.Errorf("gif: no image: %w", err)
		}
		switch b {
		case 0x21: // extension: label, then sub-blocks until a zero length
			label, err := br.ReadByte()
			if err != nil {
				return f, fmt.Errorf("gif: extension: %w", err)
			}
			pos += 2
			for first := true; ; first = false {
				n, err := br.ReadByte()
				if err != nil {
					return f, fmt.Errorf("gif: extension: %w", err)
				}
				pos++
				if n == 0 {
					break
				}
				if _, err := io.ReadFull(br, block[:n]); err != nil {
					return f, fmt.Errorf("gif: extension: %w", err)
				}
				pos += int64(n)
				if label == 0xF9 && first && n >= 4 && block[0]&1 != 0 {
					f.transparent = int(block[3])
				}
			}
		case 0x2C: // image descriptor
			f.descriptor[0] = b
			if _, err := io.ReadFull(br, f.descriptor[1:]); err != nil {
				return f, fmt.Errorf("gif: image descriptor: %w", err)
			}
			f.offset = pos
			d := f.descriptor[1:]
			x0 := int(binary.LittleEndian.Uint16(d[0:]))
			y0 := int(binary.LittleEndian.Uint16(d[2:]))
			w := int(binary.LittleEndian.Uint16(d[4:]))
			h := int(binary.LittleEndian.Uint16(d[6:]))
			f.frame = image.Rect(x0, y0, x0+w, y0+h)
			return f, nil
		default:
			return f, errors.New("gif: no image before the trailer")
		}
	}
}

func decodeGIFScreen(r io.ReaderAt, size int64) (image.Image, error) {
	f, err := parseGIFFirstFrame(io.NewSectionReader(r, 0, size))
	if err != nil {
		return nil, err
	}
	if f.frame.Empty() {
		// Pillow fails to load it, so HMA hashes nothing.
		return nil, fmt.Errorf("gif: first frame is %dx%d", f.frame.Dx(), f.frame.Dy())
	}
	// The screen grows to cover a frame that overruns it.
	w, h := max(f.screenW, f.frame.Max.X), max(f.screenH, f.frame.Max.Y)
	if err := checkPixels(w, h); err != nil {
		return nil, err
	}
	// image/gif rejects a frame outside the declared screen, and a grown
	// screen can be wider than a GIF can declare, so decode the frame alone
	// at the origin and place it here.
	origin := f.frame.Sub(f.frame.Min)
	var src io.Reader = io.NewSectionReader(r, 0, size)
	if f.frame.Min != (image.Point{}) || origin.Max != (image.Point{f.screenW, f.screenH}) {
		header, descriptor := f.header, f.descriptor
		binary.LittleEndian.PutUint16(header[6:], uint16(origin.Dx()))
		binary.LittleEndian.PutUint16(header[8:], uint16(origin.Dy()))
		binary.LittleEndian.PutUint16(descriptor[1:], 0)
		binary.LittleEndian.PutUint16(descriptor[3:], 0)
		end := f.offset + int64(len(descriptor))
		src = io.MultiReader(
			bytes.NewReader(header[:]),
			io.NewSectionReader(r, int64(len(header)), f.offset-int64(len(header))),
			bytes.NewReader(descriptor[:]),
			io.NewSectionReader(r, end, size-end))
	}
	img, err := gif.Decode(src)
	if err != nil {
		return nil, err
	}
	frame, ok := img.(*image.Paletted)
	if !ok {
		return nil, fmt.Errorf("gif: decoded %T, want *image.Paletted", img)
	}
	screen := image.Rect(0, 0, w, h)
	if f.frame == screen {
		return frame, nil
	}
	canvas := image.NewPaletted(screen, frame.Palette)
	fill := uint8(0)
	if f.transparent >= 0 {
		fill = uint8(f.transparent)
	}
	for i := range canvas.Pix {
		canvas.Pix[i] = fill
	}
	for y := range origin.Dy() {
		start := canvas.PixOffset(f.frame.Min.X, f.frame.Min.Y+y)
		copy(canvas.Pix[start:start+origin.Dx()], frame.Pix[frame.PixOffset(0, y):frame.PixOffset(origin.Dx(), y)])
	}
	return canvas, nil
}

// tiffBilevel reports whether the first IFD has one 1-bit sample per pixel,
// and whether x/image/tiff decoded it inverted: it reads a CCITT-coded
// BlackIsZero image's white runs as white, where libtiff reads them as 0 bits,
// which BlackIsZero makes black.
func tiffBilevel(r io.ReaderAt, size int64) (isBilevel, inverted bool, err error) {
	var header [8]byte
	if size < int64(len(header)) {
		return false, false, nil
	}
	if _, err := r.ReadAt(header[:], 0); err != nil {
		return false, false, err
	}
	var order binary.ByteOrder = binary.LittleEndian
	if header[0] == 'M' {
		order = binary.BigEndian
	}
	ifd := int64(order.Uint32(header[4:]))
	if ifd < 8 || ifd+2 > size {
		return false, false, nil
	}
	var count [2]byte
	if _, err := r.ReadAt(count[:], ifd); err != nil {
		return false, false, err
	}
	n := int(order.Uint16(count[:]))
	entries := make([]byte, min(int64(12*n), size-ifd-2))
	if _, err := r.ReadAt(entries, ifd+2); err != nil {
		return false, false, err
	}
	bits, samples, compression, photometric := 1, 1, 1, -1 // TIFF defaults
	for i := range n {
		e := 12 * i
		if e+12 > len(entries) {
			return false, false, nil
		}
		tag, count := order.Uint16(entries[e:]), order.Uint32(entries[e+4:])
		if tag != 258 && tag != 259 && tag != 262 && tag != 277 {
			continue
		}
		value, ok, err := tiffFirstValue(r, size, order, entries[e:e+12])
		if err != nil {
			return false, false, err
		}
		if !ok {
			continue
		}
		switch {
		case tag == 258 && count == 1:
			bits = value
		case tag == 258:
			return false, false, nil // one BitsPerSample per sample: not bilevel
		case tag == 259:
			compression = value
		case tag == 262:
			photometric = value
		case tag == 277:
			samples = value
		}
	}
	if bits != 1 || samples != 1 {
		return false, false, nil
	}
	return true, (compression == 3 || compression == 4) && photometric == 1, nil
}

// tiffFirstValue reads an IFD entry's first value when it is an integer,
// from the entry itself or, when the values do not fit in its four bytes,
// from the offset stored there. ok is false for other types and for an
// offset past the end.
func tiffFirstValue(r io.ReaderAt, size int64, order binary.ByteOrder, entry []byte) (value int, ok bool, err error) {
	var width int64
	switch order.Uint16(entry[2:]) {
	case 1, 6: // BYTE, SBYTE
		width = 1
	case 3, 8: // SHORT, SSHORT
		width = 2
	case 4, 9: // LONG, SLONG
		width = 4
	default:
		return 0, false, nil
	}
	raw := entry[8:12]
	if count := int64(order.Uint32(entry[4:])); count*width > 4 {
		offset := int64(order.Uint32(entry[8:]))
		if offset+width > size {
			return 0, false, nil
		}
		raw = make([]byte, width)
		if _, err := r.ReadAt(raw, offset); err != nil {
			return 0, false, err
		}
	}
	switch width {
	case 1:
		return int(raw[0]), true, nil
	case 2:
		return int(order.Uint16(raw)), true, nil
	}
	return int(order.Uint32(raw)), true, nil
}

// greyFromNRGBA64 recovers the grey samples of a 16-bit greyscale PNG that
// the decoder widened to NRGBA64 for its tRNS chunk.
func greyFromNRGBA64(src *image.NRGBA64) *image.Gray16 {
	b := src.Bounds()
	dst := image.NewGray16(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			s, d := src.PixOffset(x, y), dst.PixOffset(x, y)
			dst.Pix[d], dst.Pix[d+1] = src.Pix[s], src.Pix[s+1]
		}
	}
	return dst
}

// bilevel maps a 1-bit image's 0 and 255 to 0 and 1.
func bilevel(src *image.Gray) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Pix[dst.PixOffset(x, y)] = src.Pix[src.PixOffset(x, y)] / 255
		}
	}
	return dst
}

// bilevelFromNRGBA does the same for a 1-bit PNG the decoder widened to
// NRGBA for its tRNS chunk.
func bilevelFromNRGBA(src *image.NRGBA) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Pix[dst.PixOffset(x, y)] = src.Pix[src.PixOffset(x, y)] / 255
		}
	}
	return dst
}
