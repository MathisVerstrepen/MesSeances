package cinemaimage

import (
	"bytes"
	"context"
	"errors"
	"image"

	webpencoder "github.com/gen2brain/webp"
)

// Encoder allocates its compressed result internally. Raster bounds, not only
// the writer cap, bound this allocation. Codecs run synchronously under admission.
func encodePhoto(ctx context.Context, im *image.NRGBA) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	if w < 1 || h < 1 || w > MaxEdge || h > MaxEdge || im.Bounds().Min != (image.Point{}) || im.Stride != w*4 || len(im.Pix) != w*h*4 {
		return nil, ErrInvalid
	}
	var out photoOutput
	if err := webpencoder.Encode(&out, im, webpencoder.Options{Quality: 80, Method: 4, Lossless: false, Exact: false}); err != nil {
		if out.overflow || errors.Is(err, ErrTooLarge) {
			return nil, ErrTooLarge
		}
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type photoOutput struct {
	bytes.Buffer
	overflow bool
}

func (w *photoOutput) Write(p []byte) (int, error) {
	if len(p) > MaxOutput-w.Len() {
		w.overflow = true
		return 0, ErrTooLarge
	}
	return w.Buffer.Write(p)
}
