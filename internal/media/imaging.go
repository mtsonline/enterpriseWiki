// Package media implements the media library: files in categories with
// versions, rights, watermarks and an image editor.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/storage"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp" // decode WebP uploads
)

// MaxPixels limits decoded images (decompression bombs).
const MaxPixels = storage.MaxPixels

// ErrTooLarge is returned for images with too many pixels.
var ErrTooLarge = errors.New("image too large")

// CheckPixels refuses images with too many pixels (see storage).
func CheckPixels(b []byte) error { return storage.CheckPixels(b) }

// Decode reads an image after checking its dimensions.
func Decode(b []byte) (image.Image, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, "", ErrTooLarge
	}
	img, format, err := image.Decode(bytes.NewReader(b))
	return img, format, err
}

// Encode writes img in format (png, jpeg, gif; anything else as png).
func Encode(img image.Image, format string) ([]byte, string, error) {
	var buf bytes.Buffer
	var err error
	mime := "image/png"
	switch format {
	case "jpeg", "jpg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88})
		mime = "image/jpeg"
	case "gif":
		err = gif.Encode(&buf, img, nil)
		mime = "image/gif"
	default:
		err = png.Encode(&buf, img)
	}
	return buf.Bytes(), mime, err
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	stddraw.Draw(out, out.Bounds(), img, b.Min, stddraw.Src)
	return out
}

// Resize scales img to fit w×h (0 keeps the aspect ratio).
func Resize(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if w <= 0 && h <= 0 {
		return img
	}
	if w <= 0 {
		w = int(math.Round(float64(b.Dx()) * float64(h) / float64(b.Dy())))
	}
	if h <= 0 {
		h = int(math.Round(float64(b.Dy()) * float64(w) / float64(b.Dx())))
	}
	w, h = max(w, 1), max(h, 1)
	if int64(w)*int64(h) > MaxPixels {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Over, nil)
	return out
}

// Thumbnail scales img down so that its longer side is at most size.
func Thumbnail(img image.Image, size int) image.Image {
	b := img.Bounds()
	if b.Dx() <= size && b.Dy() <= size {
		return img
	}
	if b.Dx() >= b.Dy() {
		return Resize(img, size, 0)
	}
	return Resize(img, 0, size)
}

// Crop cuts the rectangle (x, y, w, h) out of img (clamped to the image).
func Crop(img image.Image, x, y, w, h int) image.Image {
	b := img.Bounds()
	r := image.Rect(b.Min.X+x, b.Min.Y+y, b.Min.X+x+w, b.Min.Y+y+h).Intersect(b)
	if r.Empty() {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	stddraw.Draw(out, out.Bounds(), img, r.Min, stddraw.Src)
	return out
}

// Rotate turns img by 90, 180 or 270 degrees clockwise.
func Rotate(img image.Image, deg int) image.Image {
	deg = ((deg % 360) + 360) % 360
	if deg != 90 && deg != 180 && deg != 270 {
		return img
	}
	src := toRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	var out *image.RGBA
	if deg == 180 {
		out = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		out = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.RGBAAt(x, y)
			switch deg {
			case 90:
				out.SetRGBA(h-1-y, x, c)
			case 180:
				out.SetRGBA(w-1-x, h-1-y, c)
			case 270:
				out.SetRGBA(y, w-1-x, c)
			}
		}
	}
	return out
}

// Flip mirrors img horizontally (or vertically).
func Flip(img image.Image, vertical bool) image.Image {
	src := toRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if vertical {
				out.SetRGBA(x, h-1-y, src.RGBAAt(x, y))
			} else {
				out.SetRGBA(w-1-x, y, src.RGBAAt(x, y))
			}
		}
	}
	return out
}

// Adjust changes brightness and contrast (-100..100), saturation
// (-100 = grey .. 100) of img.
func Adjust(img image.Image, brightness, contrast, saturation float64) image.Image {
	src := toRGBA(img)
	out := image.NewRGBA(src.Rect)
	br := brightness / 100 * 255
	ct := (100 + contrast) / 100
	ct *= ct
	sat := 1 + saturation/100
	clamp := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }
	for i := 0; i+3 < len(src.Pix); i += 4 {
		r, g, b := float64(src.Pix[i]), float64(src.Pix[i+1]), float64(src.Pix[i+2])
		grey := 0.299*r + 0.587*g + 0.114*b
		r, g, b = grey+(r-grey)*sat, grey+(g-grey)*sat, grey+(b-grey)*sat
		r, g, b = (r-128)*ct+128+br, (g-128)*ct+128+br, (b-128)*ct+128+br
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = clamp(r), clamp(g), clamp(b), src.Pix[i+3]
	}
	return out
}

// Blur applies a box blur with the given radius (three passes approximate a
// Gaussian blur). The radius is limited to 50 pixels.
func Blur(img image.Image, radius int) image.Image {
	radius = min(max(radius, 0), 50)
	if radius == 0 {
		return img
	}
	cur := toRGBA(img)
	for pass := 0; pass < 3; pass++ {
		cur = boxBlur(cur, radius, true)
		cur = boxBlur(cur, radius, false)
	}
	return cur
}

func boxBlur(src *image.RGBA, r int, horizontal bool) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(src.Rect)
	n, m := w, h
	if !horizontal {
		n, m = h, w
	}
	idx := func(line, pos int) int {
		if horizontal {
			return line*src.Stride + pos*4
		}
		return pos*src.Stride + line*4
	}
	for line := 0; line < m; line++ {
		var sum [4]int
		for k := -r; k <= r; k++ {
			p := idx(line, min(max(k, 0), n-1))
			for c := 0; c < 4; c++ {
				sum[c] += int(src.Pix[p+c])
			}
		}
		for pos := 0; pos < n; pos++ {
			o := idx(line, pos)
			for c := 0; c < 4; c++ {
				out.Pix[o+c] = uint8(min(sum[c]/(2*r+1), 255)) // #nosec G115 -- average of bytes, at most 255
			}
			add, rem := idx(line, min(pos+r+1, n-1)), idx(line, max(pos-r, 0))
			for c := 0; c < 4; c++ {
				sum[c] += int(src.Pix[add+c]) - int(src.Pix[rem+c])
			}
		}
	}
	return out
}

var (
	fontOnce sync.Once
	fontFace *opentype.Font
	fontErr  error
)

func watermarkFont() (*opentype.Font, error) {
	fontOnce.Do(func() { fontFace, fontErr = opentype.Parse(gobold.TTF) })
	return fontFace, fontErr
}

// Watermark draws lines of text diagonally over img, repeated across the
// whole image, semi-transparent with a dark outline so it shows on light
// and dark areas.
func Watermark(img image.Image, lines []string) (image.Image, error) {
	text := strings.TrimSpace(strings.Join(lines, "  ·  "))
	if text == "" {
		return img, nil
	}
	f, err := watermarkFont()
	if err != nil {
		return nil, err
	}
	src := toRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	size := math.Max(12, math.Min(float64(min(w, h))/18, 64))
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer func() { _ = face.Close() }()
	// render the text once on a strip, then stamp it rotated across the image
	adv := font.MeasureString(face, text).Ceil()
	lh := int(size * 1.4)
	strip := image.NewRGBA(image.Rect(0, 0, adv+lh, lh))
	d := &font.Drawer{Dst: strip, Face: face}
	base := fixed.I(int(size))
	for _, o := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} { // outline
		d.Src = image.NewUniform(color.RGBA{0, 0, 0, 110})
		d.Dot = fixed.Point26_6{X: fixed.I(lh/2 + o[0]), Y: base + fixed.I(o[1])}
		d.DrawString(text)
	}
	d.Src = image.NewUniform(color.RGBA{255, 255, 255, 115})
	d.Dot = fixed.Point26_6{X: fixed.I(lh / 2), Y: base}
	d.DrawString(text)

	const angle = -math.Pi / 6 // 30° upwards
	sin, cos := math.Sin(angle), math.Cos(angle)
	sw, sh := strip.Rect.Dx(), strip.Rect.Dy()
	stepX, stepY := float64(sw)+size*2, float64(sh)*3.2
	diag := math.Hypot(float64(w), float64(h))
	cx, cy := float64(w)/2, float64(h)/2
	// inverse mapping: for each image pixel find the strip pixel
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			u := dx*cos + dy*sin + diag
			v := -dx*sin + dy*cos + diag
			row := math.Floor(v / stepY)
			u += row * stepX / 2 // offset every other row
			sx, sy := int(math.Mod(u, stepX)), int(v-row*stepY)
			if sx < 0 || sx >= sw || sy < 0 || sy >= sh {
				continue
			}
			c := strip.RGBAAt(sx, sy)
			if c.A == 0 {
				continue
			}
			p := src.RGBAAt(x, y)
			a := float64(c.A) / 255
			blend := func(fg, bg uint8) uint8 { return uint8(float64(fg)*a + float64(bg)*(1-a)) }
			// strip colors are premultiplied
			fr, fg, fb := float64(c.R)/a, float64(c.G)/a, float64(c.B)/a
			src.SetRGBA(x, y, color.RGBA{blend(uint8(math.Min(fr, 255)), p.R), blend(uint8(math.Min(fg, 255)), p.G), blend(uint8(math.Min(fb, 255)), p.B), p.A})
		}
	}
	return src, nil
}

// Op is one step of the image editor.
type Op struct {
	Type string `json:"type"` // crop, resize, rotate, flip, adjust, blur, grayscale
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`
	// Deg: rotation; Vertical: flip direction; Radius: blur
	Deg        int     `json:"deg,omitempty"`
	Vertical   bool    `json:"vertical,omitempty"`
	Radius     int     `json:"radius,omitempty"`
	Brightness float64 `json:"brightness,omitempty"`
	Contrast   float64 `json:"contrast,omitempty"`
	Saturation float64 `json:"saturation,omitempty"`
}

// Apply runs the editor operations in order.
func Apply(img image.Image, ops []Op) (image.Image, error) {
	if len(ops) > 50 {
		return nil, errors.New("too many editing steps")
	}
	for _, o := range ops {
		switch o.Type {
		case "crop":
			img = Crop(img, o.X, o.Y, o.W, o.H)
		case "resize":
			if o.W > 20000 || o.H > 20000 || o.W < 0 || o.H < 0 {
				return nil, errors.New("resize: size out of range")
			}
			img = Resize(img, o.W, o.H)
		case "rotate":
			img = Rotate(img, o.Deg)
		case "flip":
			img = Flip(img, o.Vertical)
		case "adjust":
			for _, v := range []float64{o.Brightness, o.Contrast, o.Saturation} {
				if v < -100 || v > 100 {
					return nil, errors.New("adjust: values from -100 to 100")
				}
			}
			img = Adjust(img, o.Brightness, o.Contrast, o.Saturation)
		case "grayscale":
			img = Adjust(img, 0, 0, -100)
		case "blur":
			img = Blur(img, o.Radius)
		default:
			return nil, fmt.Errorf("unknown editing step %q", o.Type)
		}
	}
	return img, nil
}
