package cinemaimage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"testing"

	webpdecoder "golang.org/x/image/webp"
)

func pngPhoto(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 80, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func pngChunk(kind string, b []byte) []byte {
	out := make([]byte, 12+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], kind)
	copy(out[8:], b)
	binary.BigEndian.PutUint32(out[8+len(b):], crc32.ChecksumIEEE(out[4:8+len(b)]))
	return out
}
func addPNGChunk(b []byte, kind string, data []byte) []byte {
	out := append([]byte(nil), b[:len(b)-12]...)
	out = append(out, pngChunk(kind, data)...)
	return append(out, b[len(b)-12:]...)
}
func exifOrientation(n uint16) []byte {
	b := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(b[18:], n)
	return b
}
func TestNormalizeAspectAndSources(t *testing.T) {
	for _, tc := range []struct {
		name         string
		w, h, ow, oh int
		format       string
		orientation  uint16
	}{
		{"tiny", 1, 1, 1, 1, "png", 1}, {"portrait", 800, 2000, 640, 1600, "png", 1}, {"landscape", 2000, 800, 1600, 640, "jpeg", 1}, {"rotated", 320, 120, 120, 320, "png", 6}, {"static_webp", 32, 16, 32, 16, "webp", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := pngPhoto(t, tc.w, tc.h)
			switch tc.format {
			case "jpeg":
				im, err := png.Decode(bytes.NewReader(b))
				if err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				if jpeg.Encode(&out, im, nil) != nil {
					t.Fatal("jpeg encode")
				}
				b = out.Bytes()
			case "webp":
				p, err := Normalize(context.Background(), b, "image/png")
				if err != nil {
					t.Fatal(err)
				}
				b = p.Bytes
			}
			if tc.orientation != 1 {
				b = addPNGChunk(b, "eXIf", exifOrientation(tc.orientation))
			}
			photo, err := Normalize(context.Background(), b, "image/"+tc.format)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := webpdecoder.DecodeConfig(bytes.NewReader(photo.Bytes))
			if err != nil || cfg.Width != tc.ow || cfg.Height != tc.oh || photo.Width != tc.ow || photo.Height != tc.oh || len(photo.Bytes) > MaxOutput {
				t.Fatalf("photo=%+v config=%+v err=%v", photo, cfg, err)
			}
		})
	}
}
func TestNormalizeOrientationCoordinates(t *testing.T) {
	im := image.NewNRGBA(image.Rect(4, 5, 7, 7))
	for y := 5; y < 7; y++ {
		for x := 4; x < 7; x++ {
			im.Set(x, y, color.NRGBA{R: uint8((y-5)*3 + x - 4 + 1), A: 255})
		}
	}
	for n, want := range map[uint16][]uint8{1: {1, 2, 3, 4, 5, 6}, 2: {3, 2, 1, 6, 5, 4}, 3: {6, 5, 4, 3, 2, 1}, 4: {4, 5, 6, 1, 2, 3}, 5: {1, 4, 2, 5, 3, 6}, 6: {4, 1, 5, 2, 6, 3}, 7: {6, 3, 5, 2, 4, 1}, 8: {3, 6, 2, 5, 1, 4}} {
		o := oriented{im, n}
		index := 0
		for y := range o.Bounds().Dy() {
			for x := range o.Bounds().Dx() {
				got := o.RGBA64At(x, y)
				if got.R>>8 != uint16(want[index]) {
					t.Fatalf("orientation %d point %d got %d", n, index, got.R>>8)
				}
				index++
			}
		}
	}
}
func TestNormalizeRejectsAndStrips(t *testing.T) {
	b := pngPhoto(t, 20, 10)
	for _, tc := range []struct {
		name  string
		b     []byte
		media string
		want  error
	}{
		{"svg", []byte("<svg/>"), "image/svg+xml", ErrUnsupported}, {"gif", []byte("GIF89a"), "image/gif", ErrUnsupported},
		{"spoof", b, "image/jpeg", ErrUnsupported}, {"generic_type", b, "application/octet-stream", ErrUnsupported}, {"corrupt", b[:30], "image/png", ErrInvalid},
		{"trailing", append(append([]byte(nil), b...), 1), "image/png", ErrInvalid}, {"animation", addPNGChunk(b, "acTL", make([]byte, 8)), "image/png", ErrUnsupported},
		{"input_cap", make([]byte, MaxInput+1), "image/png", ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(context.Background(), tc.b, tc.media)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	b = addPNGChunk(b, "tEXt", []byte("source\x00private GPS marker"))
	b = addPNGChunk(b, "eXIf", exifOrientation(6))
	p, err := Normalize(context.Background(), b, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"EXIF", "XMP ", "ICCP", "private GPS marker"} {
		if bytes.Contains(p.Bytes, []byte(marker)) {
			t.Fatal("metadata retained")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Normalize(ctx, b, "image/png"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestNormalizeDimensionAndOutputCaps(t *testing.T) {
	for _, v := range []struct {
		w, h  int
		valid bool
	}{{1, 1, true}, {8192, 2048, true}, {8192, 2049, false}, {8193, 1, false}, {0, 1, false}, {1, 0, false}, {4096, 4096, true}} {
		if dimensions(v.w, v.h) != v.valid {
			t.Fatalf("dimensions %+v", v)
		}
	}
	w := photoOutput{}
	if n, e := w.Write(make([]byte, MaxOutput)); e != nil || n != MaxOutput {
		t.Fatal(e)
	}
	if _, e := w.Write([]byte{0}); !errors.Is(e, ErrTooLarge) || !w.overflow {
		t.Fatal(e)
	}
	if _, e := ReadInput(bytes.NewReader(make([]byte, MaxInput+1))); !errors.Is(e, ErrTooLarge) {
		t.Fatal(e)
	}
	if b, e := ReadInput(bytes.NewReader(make([]byte, MaxInput))); e != nil || len(b) != MaxInput {
		t.Fatal(e)
	}
	if _, e := encodePhoto(context.Background(), image.NewNRGBA(image.Rect(0, 0, MaxEdge+1, 1))); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestNormalizeWebPFraming(t *testing.T) {
	p, e := Normalize(context.Background(), pngPhoto(t, 20, 10), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = webpFraming(append(p.Bytes, 0), 20, 10); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	animated := make([]byte, 30)
	copy(animated, "RIFF")
	binary.LittleEndian.PutUint32(animated[4:], 22)
	copy(animated[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(animated[16:], 10)
	animated[20] = 2
	if _, e = webpFraming(animated, 20, 10); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e = webpFraming(p.Bytes, 19, 10); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestNormalizeRejectsEncodedOutputOverCap(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 1600, 1600))
	random := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(im.Pix); i += 4 {
		n := random.Uint32()
		im.Pix[i] = byte(n)
		im.Pix[i+1] = byte(n >> 8)
		im.Pix[i+2] = byte(n >> 16)
		im.Pix[i+3] = 255
	}
	var b bytes.Buffer
	if e := jpeg.Encode(&b, im, &jpeg.Options{Quality: 90}); e != nil {
		t.Fatal(e)
	}
	if b.Len() > MaxInput {
		t.Fatal("fixture exceeds input cap")
	}
	if _, e := Normalize(context.Background(), b.Bytes(), "image/jpeg"); !errors.Is(e, ErrTooLarge) {
		t.Fatal("encoded output cap not enforced", e)
	}
}
