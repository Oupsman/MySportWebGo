package pictures

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// picture is w x h, gray with a red top left corner, to see where it goes
func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{128, 128, 128, 255}
			if x < w/4 && y < h/4 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// withExif inserts after the SOI marker of a JPEG an EXIF segment with an orientation and a marker
// standing for the GPS data of a phone
func withExif(t *testing.T, jpg []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)
	binary.BigEndian.PutUint16(ifd[2:], 0x0112)
	binary.BigEndian.PutUint16(ifd[4:], 3) // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)
	binary.BigEndian.PutUint16(ifd[10:], orientation)
	payload := append([]byte("Exif\x00\x00"), append(append(tiff, ifd...), []byte("GPS-SECRET-48.8566N")...)...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, segment...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, data []byte) image.Image {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		t.Fatalf("output: %s, %v", format, err)
	}
	return img
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0xC000 && g < 0x4000 && b < 0x4000
}

func TestProcessTurnsUprightAndDropsMetadata(t *testing.T) {
	src := withExif(t, encodeJPEG(t, picture(400, 200)), 6)
	if jpegOrientation(src) != 6 {
		t.Fatalf("orientation read %d, want 6", jpegOrientation(src))
	}
	out, err := Process(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPS-SECRET")) {
		t.Fatal("the metadata of the upload are in the saved picture")
	}
	img := decode(t, out)
	// 6: the camera was turned, the picture is rotated a quarter clockwise
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 400 {
		t.Fatalf("size %v, want 200x400", b)
	}
	// the red top left corner is now the top right one
	if !isRed(img.At(190, 10)) || isRed(img.At(10, 10)) {
		t.Errorf("not rotated as orientation 6 says")
	}
}

func TestOrientations(t *testing.T) {
	src := picture(40, 20)
	// where the red top left corner goes, in the turned picture
	for orientation, corner := range map[int][2]int{
		1: {2, 2}, 2: {37, 2}, 3: {37, 17}, 4: {2, 17}, 5: {2, 2}, 6: {17, 2}, 7: {17, 37}, 8: {2, 37},
	} {
		img := orient(src, orientation)
		if !isRed(img.At(corner[0], corner[1])) {
			t.Errorf("orientation %d: corner not at %v", orientation, corner)
		}
	}
}

func TestProcessScalesDown(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, picture(3200, 1800)); err != nil {
		t.Fatal(err)
	}
	out, err := Process(&b)
	if err != nil {
		t.Fatal(err)
	}
	if size := decode(t, out).Bounds(); size.Dx() != MaxSide || size.Dy() != 900 {
		t.Errorf("size %v, want %dx900", size, MaxSide)
	}
	// small pictures keep their size
	out, err = Process(bytes.NewReader(encodeJPEG(t, picture(300, 200))))
	if err != nil || decode(t, out).Bounds().Dx() != 300 {
		t.Errorf("a small picture was resized: %v", err)
	}
}

func TestProcessRefuses(t *testing.T) {
	for name, data := range map[string][]byte{
		"text":        []byte("not a picture at all"),
		"pdf":         []byte("%PDF-1.4\n..."),
		"broken jpeg": encodeJPEG(t, picture(50, 50))[:60],
		"too large":   append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, MaxUploadSize)...),
	} {
		if _, err := Process(bytes.NewReader(data)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
