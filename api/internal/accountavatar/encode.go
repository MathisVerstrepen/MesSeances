package accountavatar

import (
	"bytes"
	"context"
	"image"

	webpencoder "github.com/gen2brain/webp"
)

func avatarEncodingOptions() webpencoder.Options {
	return webpencoder.Options{Quality: 80, Lossless: false, Method: 4, Exact: false}
}

// encodeAvatar accepts only the fresh, tightly packed normalized raster. The
// backend allocates its compressed output internally before calling Write;
// this writer limit is not a bound on that internal allocation.
func encodeAvatar(ctx context.Context, im *image.NRGBA) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if im.Bounds() != image.Rect(0, 0, 256, 256) || im.Stride != 256*4 || len(im.Pix) != 256*256*4 {
		return nil, ErrInvalid
	}
	var out avatarOutput
	if err := webpencoder.Encode(&out, im, avatarEncodingOptions()); err != nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type avatarOutput struct{ bytes.Buffer }

func (w *avatarOutput) Write(p []byte) (int, error) {
	if len(p) > MaxOutput-w.Len() {
		return 0, ErrTooLarge
	}
	return w.Buffer.Write(p)
}
