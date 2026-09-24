package accountavatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webpdecoder "golang.org/x/image/webp"
)

func TestConvertPNGStrictAndUpright(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var pngBytes bytes.Buffer
	im := encoderFixture(t, "alpha")
	if err = png.Encode(&pngBytes, im); err != nil {
		t.Fatal(err)
	}
	// Valid orientation metadata must be stripped, not applied again.
	tiff := tiff(6, binary.LittleEndian)
	raw := pngBytes.Bytes()
	withOrientation := append(append(append([]byte{}, raw[:33]...), pngChunk("eXIf", tiff)...), raw[33:]...)
	var small bytes.Buffer
	if err = png.Encode(&small, image.NewNRGBA(image.Rect(0, 0, 255, 256))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		mode  os.FileMode
		valid bool
	}{
		{"upright", withOrientation, 0600, true}, {"small", small.Bytes(), 0600, false},
		{"corrupt", []byte("bad"), 0600, false}, {"truncated", raw[:len(raw)-5], 0600, false},
		{"public", raw, 0644, false}, {"oversized", make([]byte, MaxOutput+1), 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := strings.Repeat("a", 32) + ".png"
			path := filepath.Join(root, name)
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			out, err := s.ConvertPNG(t.Context(), name)
			if !tc.valid {
				if err == nil {
					t.Fatal("unsafe source accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertLossyWebP(t, out)
			decoded, err := webpdecoder.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					_, _, _, got := decoded.At(x, y).RGBA()
					_, _, _, want := im.At(x, y).RGBA()
					if got != want {
						t.Fatal("alpha/orientation changed")
					}
				}
			}
			if _, err := s.Read(t.Context(), name); !errors.Is(err, ErrStorage) {
				t.Fatal("live PNG read allowed")
			}
		})
	}
	for _, name := range []string{"../escape.png", strings.Repeat("b", 32) + ".png", strings.Repeat("a", 32) + ".webp"} {
		if _, err := s.ConvertPNG(t.Context(), name); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	link := strings.Repeat("c", 32) + ".png"
	if err := os.Symlink(filepath.Join(root, strings.Repeat("a", 32)+".png"), filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConvertPNG(t.Context(), link); err == nil {
		t.Fatal("symlink accepted")
	}
}
