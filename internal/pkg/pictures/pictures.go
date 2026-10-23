// Package pictures turns the photos users upload into small JPEG files without metadata
package pictures

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png" // decoders registered for image.Decode
	"io"
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// MaxUploadSize is the size of the largest file accepted, a phone photo
	MaxUploadSize = 15 << 20
	// MaxSide is the longest side of the saved pictures, in pixels
	MaxSide = 1600
	quality = 85
)

// ErrNotAPicture is returned for files that are not JPEG, PNG or WebP pictures
var ErrNotAPicture = errors.New("the file must be a JPEG, PNG or WebP picture")

// Process reads an uploaded picture and returns it as a JPEG of at most MaxSide pixels, turned upright.
// Re-encoding drops all the metadata of the file: the EXIF of a phone photo holds where it was taken.
func Process(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxUploadSize {
		return nil, errors.New("the picture is larger than 15 MB")
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, ErrNotAPicture
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotAPicture
	}
	// scaled first: turning the few pixels left is cheap
	img = orient(fit(img, MaxSide), jpegOrientation(data))

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// fit scales img down so its longest side is at most side pixels
func fit(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img
	}
	if w >= h {
		w, h = side, h*side/w
	} else {
		w, h = w*side/h, side
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// jpegOrientation returns the EXIF orientation of a JPEG file (1 to 8), 1 when there is none.
// Phones save the photo as the sensor saw it, and this tag tells how to turn it.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		// start of the image data: the metadata segments are before
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		segment := data[i+4 : i+2+size]
		if marker == 0xE1 && len(segment) > 6 && string(segment[:6]) == "Exif\x00\x00" {
			return tiffOrientation(segment[6:])
		}
		i += 2 + size
	}
	return 1
}

// tiffOrientation reads the orientation tag (0x0112) of the first IFD of TIFF data
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}
	entries := int(order.Uint16(tiff[ifd : ifd+2]))
	for e := 0; e < entries; e++ {
		entry := ifd + 2 + e*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 {
			// a SHORT: its value is in the first 2 bytes of the value field
			value := int(order.Uint16(tiff[entry+8 : entry+10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

// orient turns img upright according to its EXIF orientation
func orient(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	// orientations 5 to 8 swap width and height
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // upside down, mirrored
				dx, dy = x, h-1-y
			case 5: // mirrored, turned left
				dx, dy = y, x
			case 6: // turned left: rotate right
				dx, dy = h-1-y, x
			case 7: // mirrored, turned right
				dx, dy = h-1-y, w-1-x
			case 8: // turned right: rotate left
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
