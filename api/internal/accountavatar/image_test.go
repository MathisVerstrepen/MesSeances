package accountavatar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"golang.org/x/image/draw"
)

// Generated locally with ImageMagick: 7x5 asymmetric corner blocks, no personal media.
const losslessFixture = "UklGRkAAAABXRUJQVlA4TDMAAAAvBgABACcgEEjaH3qN+RcQFPk/2vwHXyRsUMhIEnNfBF7hFV6g+kudRET/oy6E/+/oVAIA"
const lossyFixture = "UklGRn4AAABXRUJQVlA4IHIAAADwAQCdASoHAAUAAgA0JbACdDKAAVybFYAA/gvy1f/vf8o869//Bq17Kt/xYj8+z/4MwHz8efwP/y5C/71TP+/+/VoR//vCjuYfTv//qty+P/5sv/yGl7fyuOf3P/hOxgPw68zf67/zZtcl0fJ29p4AAAA="

func sourceImage() *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, 7, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 7; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 35), uint8(y * 50), uint8((x + y) * 20), uint8(100 + x*20)})
		}
	}
	return im
}
func fixture(t *testing.T, format string) []byte {
	t.Helper()
	var b bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&b, sourceImage(), &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
	case "png":
		if err := png.Encode(&b, sourceImage()); err != nil {
			t.Fatal(err)
		}
	default:
		raw := losslessFixture
		if format == "lossy" {
			raw = lossyFixture
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return b.Bytes()
}
func tiff(o uint16, order binary.ByteOrder) []byte {
	b := make([]byte, 26)
	copy(b, "II")
	if order == binary.BigEndian {
		copy(b, "MM")
	}
	order.PutUint16(b[2:], 42)
	order.PutUint32(b[4:], 8)
	order.PutUint16(b[8:], 1)
	order.PutUint16(b[10:], 0x112)
	order.PutUint16(b[12:], 3)
	order.PutUint32(b[14:], 1)
	order.PutUint16(b[18:], o)
	return b
}
func pngChunk(typ string, data []byte) []byte {
	b := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(b, uint32(len(data)))
	copy(b[4:], typ)
	copy(b[8:], data)
	binary.BigEndian.PutUint32(b[8+len(data):], crc32.ChecksumIEEE(b[4:8+len(data)]))
	return b
}
func webpChunk(typ string, data []byte) []byte {
	b := make([]byte, 8+len(data)+(len(data)&1))
	copy(b, typ)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(data)))
	copy(b[8:], data)
	return b
}
func metadata(b []byte, format string, payloads ...[]byte) []byte {
	out := append([]byte(nil), b...)
	for _, data := range payloads {
		switch format {
		case "jpeg":
			chunk := []byte{255, 225, 0, 0}
			binary.BigEndian.PutUint16(chunk[2:], uint16(len(data)+8))
			chunk = append(chunk, []byte("Exif\x00\x00")...)
			chunk = append(chunk, data...)
			out = append(append(append([]byte(nil), out[:2]...), chunk...), out[2:]...)
		case "png":
			out = append(append(append([]byte(nil), out[:33]...), pngChunk("eXIf", data)...), out[33:]...)
		case "webp":
			out = append(out, webpChunk("EXIF", data)...)
			binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
		}
	}
	return out
}

// Forward mapping builds an independent upright oracle, never used in production.
func expected(im image.Image, o uint16) *image.NRGBA {
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	upright := image.NewRGBA64(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch o {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			upright.Set(dx, dy, im.At(x, y))
		}
	}
	n := min(dw, dh)
	x, y := (dw-n)/2, (dh-n)/2
	out := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), upright, image.Rect(x, y, x+n, y+n), draw.Src, nil)
	return out
}
func TestOrientationFormats(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "webp"} {
		t.Run(format, func(t *testing.T) {
			original := fixture(t, format)
			im, _, err := image.Decode(bytes.NewReader(original))
			if err != nil {
				t.Fatal(err)
			}
			for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
				for o := uint16(1); o <= 8; o++ {
					payload := tiff(o, order)
					if got := orientation(t.Context(), payload); got != o {
						t.Fatalf("metadata orientation=%d want=%d", got, o)
					}
					data := metadata(original, format, payload)
					if format == "webp" && o%2 == 0 {
						data = append(append(append([]byte(nil), original...), webpChunk("JUNK", []byte{1})...), webpChunk("EXIF", append([]byte("Exif\x00\x00"), payload...))...)
						binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
					}
					got, err := Normalize(t.Context(), data, "image/"+format)
					if err != nil {
						t.Fatalf("orientation %d: %v", o, err)
					}
					decoded, err := png.Decode(bytes.NewReader(got))
					if err != nil {
						t.Fatal(err)
					}
					want := expected(im, o)
					for y := 0; y < 256; y++ {
						for x := 0; x < 256; x++ {
							a := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
							b := want.NRGBAAt(x, y)
							if a != b {
								t.Fatalf("orientation %d pixel %d,%d: %v want %v", o, x, y, a, b)
							}
						}
					}
					for pos := 8; pos < len(got); {
						n := int(binary.BigEndian.Uint32(got[pos:]))
						typ := string(got[pos+4 : pos+8])
						if typ != "IHDR" && typ != "IDAT" && typ != "IEND" {
							t.Fatalf("output metadata %s", typ)
						}
						pos += n + 12
					}
				}
			}
		})
	}
}
func TestMetadataFallbackAndBudgets(t *testing.T) {
	valid := tiff(6, binary.LittleEndian)
	wrongType := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(wrongType[12:], 4)
	array := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(array[14:], 0xffffffff)
	badOffset := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(badOffset[4:], 0xfffffffe)
	cycle := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(cycle[10:], 0x100)
	binary.LittleEndian.PutUint32(cycle[22:], 8)
	for _, bad := range [][]byte{nil, {0}, valid[:20], wrongType, array, badOffset, cycle, tiff(0, binary.LittleEndian), tiff(9, binary.BigEndian), make([]byte, 65537)} {
		if got := orientation(t.Context(), bad); got != 1 {
			t.Fatalf("bad metadata orientation=%d", got)
		}
	}
	for _, format := range []string{"jpeg", "png", "webp"} {
		b := metadata(fixture(t, format), format, valid, valid)
		exif, err := framing(b, format, 7, 5)
		if err != nil || orientation(t.Context(), exif) != 1 {
			t.Fatal("duplicate metadata")
		}
		if _, err = Normalize(t.Context(), b, "image/"+format); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if orientation(ctx, valid) != 1 {
		t.Fatal("cancel ignored")
	}
	r := &metadataReader{ctx: t.Context(), r: bytes.NewReader(valid)}
	if _, err := r.Seek(100, io.SeekStart); err == nil {
		t.Fatal("out of range seek")
	}
	if _, err := r.Seek(0, io.SeekStart); err == nil {
		t.Fatal("sticky budget failure")
	}
	r = &metadataReader{ctx: t.Context(), r: bytes.NewReader(valid)}
	for range 4096 {
		_, _ = r.Seek(0, io.SeekStart)
	}
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Fatal("operation budget")
	}
	r = &metadataReader{ctx: t.Context(), r: bytes.NewReader(valid)}
	if _, err := r.Read(make([]byte, (256<<10)+1)); err == nil {
		t.Fatal("byte budget")
	}
}
func TestImageBoundsAndAttackFrames(t *testing.T) {
	if !dimensions(8192, 2048) || dimensions(8192, 2049) || dimensions(0, 1) || dimensions(8193, 1) {
		t.Fatal("dimension bounds")
	}
	for _, format := range []string{"jpeg", "png", "webp", "lossy"} {
		b := fixture(t, format)
		media := format
		if format == "lossy" {
			media = "webp"
		}
		if _, err := Normalize(t.Context(), b, "image/"+media); err != nil {
			t.Fatal(format, err)
		}
		for _, bad := range [][]byte{b[:len(b)-2], append(append([]byte(nil), b...), []byte("<script>")...)} {
			if _, err := Normalize(t.Context(), bad, "image/"+media); err == nil {
				t.Fatal("bad framing accepted", format)
			}
		}
		if _, err := Normalize(t.Context(), b, "image/gif"); !errors.Is(err, ErrUnsupported) {
			t.Fatal("MIME mismatch")
		}
	}
	for _, b := range [][]byte{[]byte("GIF89a"), []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), make([]byte, MaxInput+1)} {
		if _, err := Normalize(t.Context(), b, "image/png"); err == nil {
			t.Fatal("unsupported accepted")
		}
	}
	b := fixture(t, "png")
	apng := append(append(append([]byte(nil), b[:33]...), pngChunk("acTL", make([]byte, 8))...), b[33:]...)
	if _, err := Normalize(t.Context(), apng, "image/png"); !errors.Is(err, ErrUnsupported) {
		t.Fatal("APNG")
	}
	b = fixture(t, "webp")
	animated := append(b, webpChunk("ANIM", make([]byte, 6))...)
	binary.LittleEndian.PutUint32(animated[4:], uint32(len(animated)-8))
	if _, err := Normalize(t.Context(), animated, "image/webp"); !errors.Is(err, ErrUnsupported) {
		t.Fatal("animation")
	}
	canvas := make([]byte, 10)
	canvas[4] = 6
	canvas[7] = 5
	b = fixture(t, "webp")
	b = append(append(append([]byte(nil), b[:12]...), webpChunk("VP8X", canvas)...), b[12:]...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	if _, err := Normalize(t.Context(), b, "image/webp"); !errors.Is(err, ErrInvalid) {
		t.Fatal("inconsistent canvas")
	}
	if b, err := ReadInput(bytes.NewReader(make([]byte, MaxInput))); err != nil || len(b) != MaxInput {
		t.Fatal("exact input limit")
	}
	if _, err := ReadInput(bytes.NewReader(make([]byte, MaxInput+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatal("actual limit")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Normalize(ctx, fixture(t, "png"), "image/png"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
}
