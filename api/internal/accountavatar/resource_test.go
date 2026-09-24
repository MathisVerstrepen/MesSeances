package accountavatar

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func FuzzNormalize(f *testing.F) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3, 5))); err != nil {
		f.Fatal(err)
	}
	f.Add(b.Bytes(), "image/png")
	f.Add([]byte("\xff\xd8\xff\xd9"), "image/jpeg")
	f.Add([]byte("RIFF\x04\x00\x00\x00WEBP"), "image/webp")
	f.Fuzz(func(t *testing.T, b []byte, media string) {
		if len(b) > 65536 {
			t.Skip()
		}
		out, err := Normalize(t.Context(), b, media)
		if err != nil {
			return
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(out))
		if err != nil || cfg.Width != 256 || cfg.Height != 256 || len(out) > MaxOutput {
			t.Fatal("normalized image invariant")
		}
		meta, err := pngFraming(out)
		if err != nil || len(meta) != 0 {
			t.Fatal("metadata survived normalization")
		}
	})
}
func FuzzOrientationMetadata(f *testing.F) {
	f.Add([]byte("II*\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("MM\x00*\xff\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 64<<10 {
			t.Skip()
		}
		o := orientation(t.Context(), b)
		if o < 1 || o > 8 {
			t.Fatal("invalid orientation")
		}
	})
}

// One-shot resource probe, not a default heavy unit test. The decoded 16-bit
// raster alone is 128 MiB; a second rotated full raster would exceed 256 MiB.
func BenchmarkNormalizeMaxRaster(b *testing.B) {
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewNRGBA64(image.Rect(0, 0, 4096, 4096))); err != nil {
		b.Fatal(err)
	}
	// TIFF orientation 6, little-endian. Orientation fixtures test every mapping.
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	raw := input.Bytes()
	raw = append(append(append([]byte{}, raw[:33]...), pngChunk("eXIf", tiff)...), raw[33:]...)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Normalize(context.Background(), raw, "image/png"); err != nil {
			b.Fatal(err)
		}
	}
}
