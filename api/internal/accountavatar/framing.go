package accountavatar

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"io"
)

// Framing only: raster decoding remains with maintained standard/x/image codecs.
// A second EXIF block disables optional metadata; it never selects a later value.
type exifBlock struct {
	data  []byte
	count int
}

func (e *exifBlock) add(b []byte) {
	e.count++
	if e.count == 1 && len(b) <= 64<<10 {
		e.data = b
	} else {
		e.data = nil
	}
}
func framing(b []byte, format string, w, h int) ([]byte, error) {
	switch format {
	case "png":
		return pngFraming(b)
	case "jpeg":
		return jpegFraming(b)
	case "webp":
		return webpFraming(b, w, h)
	}
	return nil, ErrUnsupported
}
func pngFraming(b []byte) ([]byte, error) {
	if len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, ErrInvalid
	}
	var exif exifBlock
	for pos := 8; pos < len(b); {
		if len(b)-pos < 12 {
			return nil, ErrInvalid
		}
		n := int(binary.BigEndian.Uint32(b[pos:]))
		if n > len(b)-pos-12 {
			return nil, ErrInvalid
		}
		typ := string(b[pos+4 : pos+8])
		data := b[pos+8 : pos+8+n]
		if crc32.ChecksumIEEE(b[pos+4:pos+8+n]) != binary.BigEndian.Uint32(b[pos+8+n:]) {
			return nil, ErrInvalid
		}
		pos += n + 12
		switch typ {
		case "acTL", "fcTL", "fdAT":
			return nil, ErrUnsupported
		case "eXIf":
			exif.add(data)
		case "IEND":
			if n != 0 || pos != len(b) {
				return nil, ErrInvalid
			}
			return exif.data, nil
		}
	}
	return nil, ErrInvalid
}
func jpegFraming(b []byte) ([]byte, error) {
	if len(b) < 4 || b[0] != 255 || b[1] != 216 {
		return nil, ErrInvalid
	}
	var exif exifBlock
	scan := false
	for pos := 2; pos < len(b); {
		if scan {
			for pos < len(b) && b[pos] != 255 {
				pos++
			}
		}
		if pos >= len(b) || b[pos] != 255 {
			return nil, ErrInvalid
		}
		for pos < len(b) && b[pos] == 255 {
			pos++
		}
		if pos >= len(b) {
			return nil, ErrInvalid
		}
		marker := b[pos]
		pos++
		if scan && (marker == 0 || marker >= 208 && marker <= 215) {
			continue
		}
		scan = false
		if marker == 217 {
			if pos != len(b) {
				return nil, ErrInvalid
			}
			return exif.data, nil
		}
		if marker == 0 || marker == 216 || marker == 1 || marker >= 208 && marker <= 215 || len(b)-pos < 2 {
			return nil, ErrInvalid
		}
		n := int(binary.BigEndian.Uint16(b[pos:]))
		if n < 2 || n > len(b)-pos {
			return nil, ErrInvalid
		}
		data := b[pos+2 : pos+n]
		if marker == 225 && bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
			exif.add(data[6:])
		}
		pos += n
		if marker == 218 {
			scan = true
		}
	}
	return nil, ErrInvalid
}
func webpFraming(b []byte, w, h int) ([]byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(b[4:]))+8 != uint64(len(b)) {
		return nil, ErrInvalid
	}
	var exif exifBlock
	frames, extended := 0, false
	for pos := 12; pos < len(b); {
		if len(b)-pos < 8 {
			return nil, ErrInvalid
		}
		typ := string(b[pos : pos+4])
		n := int(binary.LittleEndian.Uint32(b[pos+4:]))
		start := pos + 8
		if n > len(b)-start {
			return nil, ErrInvalid
		}
		data := b[start : start+n]
		pos = start + n + (n & 1)
		if pos > len(b) || n&1 != 0 && b[pos-1] != 0 {
			return nil, ErrInvalid
		}
		switch typ {
		case "VP8X":
			if start != 20 || extended || n != 10 {
				return nil, ErrInvalid
			}
			extended = true
			if data[0]&2 != 0 {
				return nil, ErrUnsupported
			}
			if data[0]&0xc1 != 0 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
				return nil, ErrInvalid
			}
			cw := 1 + int(data[4]) + int(data[5])<<8 + int(data[6])<<16
			ch := 1 + int(data[7]) + int(data[8])<<8 + int(data[9])<<16
			if cw != w || ch != h || !dimensions(cw, ch) {
				return nil, ErrInvalid
			}
		case "ANIM", "ANMF":
			return nil, ErrUnsupported
		case "VP8 ", "VP8L":
			frames++
			if frames != 1 {
				return nil, ErrInvalid
			}
			// Decode just the inner frame's configuration before any full allocation.
			header := make([]byte, 20)
			copy(header, "RIFF")
			binary.LittleEndian.PutUint32(header[4:], uint32(n+12+(n&1)))
			copy(header[8:], "WEBP")
			copy(header[12:], typ)
			binary.LittleEndian.PutUint32(header[16:], uint32(n))
			cfg, _, err := image.DecodeConfig(io.MultiReader(bytes.NewReader(header), bytes.NewReader(data), bytes.NewReader([]byte{0})))
			if err != nil || cfg.Width != w || cfg.Height != h || !dimensions(cfg.Width, cfg.Height) {
				return nil, ErrInvalid
			}
		case "EXIF":
			exif.add(bytes.TrimPrefix(data, []byte("Exif\x00\x00")))
		}
	}
	if frames != 1 {
		return nil, ErrInvalid
	}
	return exif.data, nil
}
