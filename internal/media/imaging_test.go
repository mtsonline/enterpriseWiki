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
