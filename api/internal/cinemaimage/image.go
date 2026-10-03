// Package cinemaimage owns private admin cinema photos, not public schedule projections.
package cinemaimage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime"

	"github.com/bep/imagemeta"
	"golang.org/x/image/draw"
	webpdecoder "golang.org/x/image/webp"
)

const (
	MaxInput          = 5 << 20
	MaxBody           = MaxInput + 64<<10
	MaxOutput         = 1 << 20
	MaxEdge           = 1600
	MaxRevision int64 = 9007199254740991
)

var (
	ErrRequest           = errors.New("invalid cinema image request")
	ErrTooLarge          = errors.New("cinema image too large")
	ErrUnsupported       = errors.New("cinema image unsupported")
	ErrInvalid           = errors.New("cinema image invalid")
	ErrBusy              = errors.New("cinema image busy")
	ErrStorage           = errors.New("cinema images unavailable")
	ErrURL               = errors.New("invalid image url")
	ErrDownload          = errors.New("cinema image download failed")
	ErrImportUnavailable = errors.New("cinema image import unavailable")
	ErrNotFound          = errors.New("theater not found")
	ErrImageNotFound     = errors.New("cinema image not found")
	ErrConflict          = errors.New("cinema image conflict")
)

func dimensions(w, h int) bool { return w > 0 && h > 0 && w <= 8192 && h <= 8192 && w <= 16777216/h }

type Photo struct {
	Bytes         []byte
	Width, Height int
}

// Normalize retains only a fresh raster, never source metadata. Caller holds admission.
func Normalize(ctx context.Context, b []byte, contentType string) (Photo, error) {
	if err := ctx.Err(); err != nil {
		return Photo{}, err
	}
	if len(b) > MaxInput {
		return Photo{}, ErrTooLarge
	}
	cfg, format, err := decodeConfig(b)
	if errors.Is(err, ErrUnsupported) {
		return Photo{}, err
	}
	if err != nil || !dimensions(cfg.Width, cfg.Height) {
		return Photo{}, ErrInvalid
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil || media != "image/"+format {
		return Photo{}, ErrUnsupported
	}
	exif, err := framing(b, format, cfg.Width, cfg.Height)
	if err != nil {
		return Photo{}, err
	}
	o := orientation(ctx, exif)
	if err = ctx.Err(); err != nil {
		return Photo{}, err
	}
	im, err := decodeRaster(b, format)
	if err != nil || im.Bounds().Dx() != cfg.Width || im.Bounds().Dy() != cfg.Height {
		return Photo{}, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return Photo{}, err
	}
	out := normalizedRaster(im, o)
	encoded, err := encodePhoto(ctx, out)
	if err != nil {
		return Photo{}, err
	}
	return Photo{Bytes: encoded, Width: out.Bounds().Dx(), Height: out.Bounds().Dy()}, nil
}

// Never use image.Decode's registry: the encoder registers another WebP decoder.
func decodeConfig(b []byte) (image.Config, string, error) {
	r := bytes.NewReader(b)
	switch {
	case bytes.HasPrefix(b, []byte{255, 216}):
		c, e := jpeg.DecodeConfig(r)
		return c, "jpeg", e
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		c, e := png.DecodeConfig(r)
		return c, "png", e
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		c, e := webpdecoder.DecodeConfig(r)
		return c, "webp", e
	default:
		return image.Config{}, "", ErrUnsupported
	}
}
func decodeRaster(b []byte, f string) (image.Image, error) {
	r := bytes.NewReader(b)
	switch f {
	case "jpeg":
		return jpeg.Decode(r)
	case "png":
		return png.Decode(r)
	case "webp":
		return webpdecoder.Decode(r)
	default:
		return nil, ErrUnsupported
	}
}
func normalizedRaster(im image.Image, orientation uint16) *image.NRGBA {
	upright := oriented{im, orientation}
	b := upright.Bounds()
	w, h := b.Dx(), b.Dy()
	if edge := max(w, h); edge > MaxEdge {
		w = max(1, w*MaxEdge/edge)
		h = max(1, h*MaxEdge/edge)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), upright, b, draw.Src, nil)
	return out
}

// Inverse coordinate view avoids allocating a full-size rotated raster.
type oriented struct {
	image.Image
	orientation uint16
}

var _ image.RGBA64Image = oriented{}

func (o oriented) RGBA64At(x, y int) color.RGBA64 {
	x, y = o.sourcePoint(x, y)
	if s, ok := o.Image.(image.RGBA64Image); ok {
		return s.RGBA64At(x, y)
	}
	return color.RGBA64Model.Convert(o.Image.At(x, y)).(color.RGBA64)
}
func (o oriented) Bounds() image.Rectangle {
	b := o.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	if o.orientation >= 5 {
		w, h = h, w
	}
	return image.Rect(0, 0, w, h)
}
func (o oriented) At(x, y int) color.Color { x, y = o.sourcePoint(x, y); return o.Image.At(x, y) }
func (o oriented) sourcePoint(x, y int) (int, int) {
	b := o.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return b.Min.X + x, b.Min.Y + y
}

type metadataReader struct {
	ctx          context.Context
	r            *bytes.Reader
	calls, reads int
	failed       bool
}

func (r *metadataReader) check() error {
	r.calls++
	if r.failed || r.calls > 4096 || r.reads > 256<<10 || r.ctx.Err() != nil {
		r.failed = true
		return ErrInvalid
	}
	return nil
}
func (r *metadataReader) Read(p []byte) (int, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	if len(p) > (256<<10)-r.reads {
		r.failed = true
		return 0, ErrInvalid
	}
	n, e := r.r.Read(p)
	r.reads += n
	return n, e
}
func (r *metadataReader) Seek(offset int64, whence int) (int64, error) {
	if e := r.check(); e != nil {
		return 0, e
	}
	n, e := r.r.Seek(offset, whence)
	if e != nil || n < 0 || n > r.r.Size() {
		r.failed = true
		return 0, ErrInvalid
	}
	return n, nil
}
func orientation(ctx context.Context, b []byte) uint16 {
	if len(b) == 0 || len(b) > 64<<10 {
		return 1
	}
	r := &metadataReader{ctx: ctx, r: bytes.NewReader(b)}
	v := uint16(1)
	_, err := imagemeta.Decode(imagemeta.Options{R: r, ImageFormat: imagemeta.TIFF, Sources: imagemeta.EXIF, LimitTagSize: 2, LimitNumTags: 256,
		Warnf:           func(string, ...any) {},
		ShouldHandleTag: func(t imagemeta.TagInfo) bool { return t.Namespace == "IFD0" && t.Tag == "Orientation" },
		HandleTag: func(t imagemeta.TagInfo) error {
			if n, ok := t.Value.(uint16); ok && n >= 1 && n <= 8 {
				v = n
			}
			return imagemeta.ErrStopWalking
		},
	})
	if err != nil || r.failed {
		return 1
	}
	return v
}
func ReadInput(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if len(b) > MaxInput {
		return nil, ErrTooLarge
	}
	return b, e
}
