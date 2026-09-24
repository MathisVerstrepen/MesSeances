// Package accountavatar handles private, bounded avatar media. It owns no account authority.
package accountavatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"

	"github.com/bep/imagemeta"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxInput  = 5 << 20
	MaxOutput = 512 << 10
	MaxBody   = MaxInput + 64<<10
)

var (
	ErrTooLarge    = errors.New("avatar too large")
	ErrUnsupported = errors.New("avatar unsupported")
	ErrInvalid     = errors.New("avatar invalid")
	ErrBusy        = errors.New("avatar busy")
	ErrStorage     = errors.New("avatar storage unavailable")
)

func dimensions(w, h int) bool { return w > 0 && h > 0 && w <= 8192 && h <= 8192 && w <= 16777216/h }

// Normalize retains only a new raster, not source bytes or metadata. Callers hold admission.
func Normalize(ctx context.Context, data []byte, contentType string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > MaxInput {
		return nil, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, ErrUnsupported
	}
	if err != nil || !dimensions(cfg.Width, cfg.Height) {
		return nil, ErrInvalid
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil || media != "image/"+format {
		return nil, ErrUnsupported
	}
	exif, err := framing(data, format, cfg.Width, cfg.Height)
	if err != nil {
		return nil, err
	}
	o := orientation(ctx, exif)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	im, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format || im.Bounds().Dx() != cfg.Width || im.Bounds().Dy() != cfg.Height {
		return nil, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	upright := oriented{im, o}
	b := upright.Bounds()
	size := min(b.Dx(), b.Dy())
	x, y := (b.Dx()-size)/2, (b.Dy()-size)/2
	out := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), upright, image.Rect(x, y, x+size, y+size), draw.Src, nil)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err = png.Encode(&buf, out); err != nil || buf.Len() > MaxOutput {
		return nil, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// oriented is a nonallocating inverse coordinate transform, including reflections.
type oriented struct {
	image.Image
	orientation uint16
}

// x/image/draw's NRGBA destination dispatch requires an RGBA64Image source.
// Keep this interface explicit: a plain Image view silently selects no scaler.
var _ image.RGBA64Image = oriented{}

func (o oriented) RGBA64At(x, y int) color.RGBA64 {
	x, y = o.sourcePoint(x, y)
	if source, ok := o.Image.(image.RGBA64Image); ok {
		return source.RGBA64At(x, y)
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
func (o oriented) At(x, y int) color.Color {
	x, y = o.sourcePoint(x, y)
	return o.Image.At(x, y)
}
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

// metadataReader bounds parser work and prevents seeking outside the isolated payload.
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
	n, err := r.r.Read(p)
	r.reads += n
	return n, err
}
func (r *metadataReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	n, err := r.r.Seek(offset, whence)
	if err != nil || n < 0 || n > r.r.Size() {
		r.failed = true
		return 0, ErrInvalid
	}
	return n, nil
}
func orientation(ctx context.Context, payload []byte) uint16 {
	if len(payload) == 0 || len(payload) > 64<<10 {
		return 1
	}
	r := &metadataReader{ctx: ctx, r: bytes.NewReader(payload)}
	value := uint16(1)
	_, err := imagemeta.Decode(imagemeta.Options{R: r, ImageFormat: imagemeta.TIFF, Sources: imagemeta.EXIF, LimitTagSize: 2, LimitNumTags: 256,
		Warnf:           func(string, ...any) {},
		ShouldHandleTag: func(t imagemeta.TagInfo) bool { return t.Namespace == "IFD0" && t.Tag == "Orientation" },
		HandleTag: func(t imagemeta.TagInfo) error {
			if v, ok := t.Value.(uint16); ok && v >= 1 && v <= 8 {
				value = v
			}
			return imagemeta.ErrStopWalking
		},
	})
	if err != nil || r.failed {
		return 1
	}
	return value
}

// ReadInput detects oversize even without Content-Length and does not spool originals.
func ReadInput(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if len(b) > MaxInput {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}
