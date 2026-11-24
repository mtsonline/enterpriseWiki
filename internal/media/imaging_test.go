package media

import (
	"image"
	"image/color"
	"testing"
)

func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 80, 255})
		}
	}
	return img
}

func TestEditing(t *testing.T) {
	img := testImage(200, 100)
	out, err := Apply(img, []Op{{Type: "crop", X: 10, Y: 10, W: 100, H: 50}, {Type: "rotate", Deg: 90}, {Type: "resize", W: 25},
		{Type: "flip"}, {Type: "adjust", Brightness: 10, Contrast: 20, Saturation: -50}, {Type: "blur", Radius: 2}, {Type: "grayscale"}})
	if err != nil {
		t.Fatal(err)
	}
	if b := out.Bounds(); b.Dx() != 25 || b.Dy() != 50 {
		t.Fatalf("size %v", b)
	}
	c := toRGBA(out).RGBAAt(5, 5)
	if c.R != c.G || c.G != c.B {
		t.Fatalf("not grey: %v", c)
	}
	if _, err := Apply(img, []Op{{Type: "explode"}}); err == nil {
		t.Fatal("unknown step accepted")
	}
	if _, err := Apply(img, []Op{{Type: "resize", W: 900000}}); err == nil {
		t.Fatal("huge resize accepted")
	}
	if _, err := Apply(img, []Op{{Type: "adjust", Brightness: 500}}); err == nil {
		t.Fatal("out of range accepted")
	}
}

func TestWatermark(t *testing.T) {
	img := testImage(400, 300)
	before := img.RGBAAt(200, 150)
	out, err := Watermark(testImage(400, 300), []string{"Erika Müller", "2026-11-06"})
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	r := toRGBA(out)
	for y := 0; y < 300; y += 3 {
		for x := 0; x < 400; x += 3 {
			if r.RGBAAt(x, y) != testImage(400, 300).RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed < 500 {
		t.Fatalf("watermark barely visible: %d pixels", changed)
	}
	_ = before
	if same, _ := Watermark(img, nil); same != image.Image(img) {
		t.Fatal("empty watermark changed the image")
	}
}

func TestDecodeLimits(t *testing.T) {
	b, _, err := Encode(testImage(10, 10), "png")
	if err != nil {
		t.Fatal(err)
	}
	if _, f, err := Decode(b); err != nil || f != "png" {
		t.Fatal(f, err)
	}
	if _, _, err := Decode([]byte("not an image")); err == nil {
		t.Fatal("garbage decoded")
	}
}

// TestRedact: the area is gone for good (black, or blocks of one colour),
// everything outside stays exactly as it was.
func TestRedact(t *testing.T) {
	img := testImage(100, 60)
	out, err := Apply(img, []Op{{Type: "redact", X: 10, Y: 10, W: 30, H: 20, Mode: "fill"}})
	if err != nil {
		t.Fatal(err)
	}
	o := toRGBA(out)
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			in := x >= 10 && x < 40 && y >= 10 && y < 30
			got := o.RGBAAt(x, y)
			if in && got != (color.RGBA{0, 0, 0, 255}) {
				t.Fatalf("fill: %d,%d is %v", x, y, got)
			}
			if !in && got != img.RGBAAt(x, y) {
				t.Fatalf("fill changed %d,%d outside the area", x, y)
			}
		}
	}
	// pixelate: blocks of at least 12 pixels, even when a smaller size is asked for
	out, err = Apply(img, []Op{{Type: "redact", X: 0, Y: 0, W: 48, H: 24, Mode: "pixelate", Size: 2}})
	if err != nil {
		t.Fatal(err)
	}
	o = toRGBA(out)
	for by := 0; by < 24; by += 12 {
		for bx := 0; bx < 48; bx += 12 {
			first := o.RGBAAt(bx, by)
			for y := by; y < by+12; y++ {
				for x := bx; x < bx+12; x++ {
					if o.RGBAAt(x, y) != first {
						t.Fatalf("block %d,%d not of one colour", bx, by)
					}
				}
			}
		}
	}
	if o.RGBAAt(60, 40) != img.RGBAAt(60, 40) {
		t.Fatal("pixelate changed the outside")
	}
	// an area partly outside is clamped; one fully outside, other modes and huge values are refused
	if _, err := Apply(img, []Op{{Type: "redact", X: 90, Y: 50, W: 500, H: 500, Mode: "fill"}}); err != nil {
		t.Fatalf("clamped area: %v", err)
	}
	for _, op := range []Op{
		{Type: "redact", X: 200, Y: 0, W: 10, H: 10, Mode: "fill"},
		{Type: "redact", X: 0, Y: 0, W: 10, H: 10, Mode: "blur"},
		{Type: "redact", X: 0, Y: 0, W: 0, H: 10, Mode: "fill"},
		{Type: "redact", X: 0, Y: 0, W: 2_000_000, H: 10, Mode: "fill"},
		{Type: "crop", X: -5, Y: 0, W: 10, H: 10},
	} {
		if _, err := Apply(img, []Op{op}); err == nil {
			t.Errorf("%+v accepted", op)
		}
	}
}
