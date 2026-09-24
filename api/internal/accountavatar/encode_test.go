package accountavatar

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	webpencoder "github.com/gen2brain/webp"
	webpdecoder "golang.org/x/image/webp"
)

// Photographic fixture supplied by the pinned Go toolchain, not personal media:
// https://go.googlesource.com/go/+/refs/tags/go1.25.13/src/image/testdata/video-001.jpeg
// Copyright The Go Authors, BSD-3-Clause (GOROOT/LICENSE). Reading the installed
// testdata avoids duplicating licensed binary data in application sources.
func encoderFixture(tb testing.TB, kind string) *image.NRGBA {
	tb.Helper()
	if kind == "photo" {
		root, err := exec.CommandContext(tb.Context(), "go", "env", "GOROOT").Output()
		if err != nil {
			tb.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), "src/image/testdata/video-001.jpeg"))
		if err != nil {
			tb.Fatal(err)
		}
		im, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			tb.Fatal(err)
		}
		return expected(im, 1)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	state := uint32(17)
	for y := range 256 {
		for x := range 256 {
			c := color.NRGBA{uint8(x), uint8(y), 80, uint8(x)}
			if kind == "entropy" {
				state = state*1664525 + 1013904223
				c = color.NRGBA{uint8(state), uint8(state >> 8), uint8(state >> 16), 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	return im
}

func assertLossyWebP(t *testing.T, b []byte) {
	t.Helper()
	if _, err := webpFraming(b, 256, 256); err != nil {
		t.Fatal(err)
	}
	frames := 0
	for pos := 12; pos < len(b); {
		n := int(binary.LittleEndian.Uint32(b[pos+4:]))
		switch typ := string(b[pos : pos+4]); typ {
		case "VP8 ":
			frames++
		case "VP8X", "ALPH":
		default:
			t.Fatalf("unexpected output chunk %q", typ)
		}
		pos += 8 + n + (n & 1)
	}
	if frames != 1 {
		t.Fatal("output is not one lossy VP8 image")
	}
}

func TestAvatarWebPEncoder(t *testing.T) {
	if err := webpencoder.Dynamic(); err == nil || err.Error() != "webp: dynamic disabled" {
		t.Fatal("build requires -tags=nodynamic")
	}
	opts := avatarEncodingOptions()
	if opts.Quality != 80 || opts.Method != 4 || opts.Lossless || opts.Exact || opts.AutoRotate {
		t.Fatal("encoding contract changed")
	}
	for _, kind := range []string{"photo", "alpha", "entropy"} {
		t.Run(kind, func(t *testing.T) {
			im := encoderFixture(t, kind)
			start := time.Now()
			b, err := encodeAvatar(t.Context(), im)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			assertLossyWebP(t, b)
			decoded, err := webpdecoder.Decode(bytes.NewReader(b))
			if err != nil || decoded.Bounds() != im.Bounds() {
				t.Fatal("output decode", err)
			}
			var pngBytes bytes.Buffer
			if err = png.Encode(&pngBytes, im); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s png=%d webp=%d encode=%s", kind, pngBytes.Len(), len(b), elapsed)
			if kind == "photo" && len(b) >= pngBytes.Len() {
				t.Fatal("photographic output not smaller than normalized PNG")
			}
			var squared float64
			var samples int
			for y := range 256 {
				for x := range 256 {
					a := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					want := im.NRGBAAt(x, y)
					if a.A != want.A {
						t.Fatalf("alpha %d,%d: %d want %d", x, y, a.A, want.A)
					}
					if want.A != 0 {
						for _, pair := range [][2]uint8{{a.R, want.R}, {a.G, want.G}, {a.B, want.B}} {
							d := float64(pair[0]) - float64(pair[1])
							squared += d * d
							samples++
						}
					}
				}
			}
			// Mean squared channel error <= 225 (RMSE <= 15) for the
			// photograph/gradient, not for deliberately incompressible noise.
			if kind != "entropy" && squared/float64(samples) > 225 {
				t.Fatalf("fidelity MSE %.2f", squared/float64(samples))
			}
		})
	}
}

func BenchmarkAvatarWebP(b *testing.B) {
	for _, kind := range []string{"photo", "alpha", "entropy"} {
		b.Run(kind, func(b *testing.B) {
			im := encoderFixture(b, kind)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := encodeAvatar(b.Context(), im); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestAvatarWebPTwoOperations(t *testing.T) {
	im := encoderFixture(t, "photo")
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := encodeAvatar(t.Context(), im); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

// Measurements include input decode, orientation/crop/scale and encoding. The
// two-slot benchmark reports time/allocations per batch of two, not per image.
func BenchmarkAvatarWebPNormalize(b *testing.B) {
	im := encoderFixture(b, "photo")
	var raw bytes.Buffer
	if err := png.Encode(&raw, im); err != nil {
		b.Fatal(err)
	}
	for _, concurrency := range []int{1, 2} {
		b.Run(fmt.Sprintf("operations-%d", concurrency), func(b *testing.B) {
			s := &Store{slots: make(chan struct{}, 2)}
			b.ReportAllocs()
			for b.Loop() {
				var wg sync.WaitGroup
				for range concurrency {
					wg.Go(func() {
						release, err := s.Admit(b.Context())
						if err != nil {
							b.Error(err)
							return
						}
						defer release()
						if _, err := Normalize(b.Context(), raw.Bytes(), "image/png"); err != nil {
							b.Error(err)
						}
					})
				}
				wg.Wait()
			}
		})
	}
}
