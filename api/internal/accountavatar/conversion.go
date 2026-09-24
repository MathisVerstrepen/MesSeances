package accountavatar

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
)

// LegacyPNGName identifies only generated legacy references, never paths.
func LegacyPNGName(name string) bool { return legacyName.MatchString(name) }

func storedName(name string) bool { return finalName.MatchString(name) || legacyName.MatchString(name) }

// ConvertPNG is for the quiesced offline converter only. Callers hold admission.
// Stored PNGs are already upright and cropped: never orient or resize them again.
// Normal Read intentionally has no PNG compatibility path.
func (s *Store) ConvertPNG(ctx context.Context, name string) ([]byte, error) {
	if !legacyName.MatchString(name) {
		return nil, ErrStorage
	}
	b, err := s.readFile(ctx, name)
	if err != nil {
		return nil, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		return nil, ErrStorage
	}
	if _, err = pngFraming(b); err != nil {
		return nil, ErrStorage
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil || im.Bounds() != image.Rect(0, 0, 256, 256) {
		return nil, ErrStorage
	}
	out := image.NewNRGBA(im.Bounds())
	draw.Draw(out, out.Bounds(), im, image.Point{}, draw.Src)
	return encodeAvatar(ctx, out)
}
