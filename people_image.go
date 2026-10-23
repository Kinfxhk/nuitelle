package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
	_ "image/gif"
)

// Portrait upload limits and output sizes. Everything is processed locally:
// decode, auto-orient from EXIF, downscale, re-encode as JPEG.
const (
	maxPersonUploadBytes = 15 << 20
	maxPersonPixels      = 40_000_000
	personFullSide       = 1600
	personThumbWidth     = 320
	personJPEGQuality    = 88
	personThumbQuality   = 85
)

// readPersonImageFile loads a local image for import. Only regular files up to
// the upload cap are accepted; the bytes still have to decode as an image.
func readPersonImageFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("path must be absolute")
	}
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("cannot read file")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("file is empty")
	}
	if info.Size() > maxPersonUploadBytes {
		return nil, fmt.Errorf("file larger than %d MB", maxPersonUploadBytes>>20)
	}
	return os.ReadFile(clean)
}

// processPersonImage turns arbitrary upload bytes into a portrait JPEG and a
// thumbnail. Both are returned encoded.
func processPersonImage(data []byte) (full, thumb []byte, err error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("empty image")
	}
	if len(data) > maxPersonUploadBytes {
		return nil, nil, fmt.Errorf("image larger than %d MB", maxPersonUploadBytes>>20)
	}
	img, _, derr := image.Decode(bytes.NewReader(data))
	if derr != nil {
		return nil, nil, fmt.Errorf("unsupported image: %w", derr)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, nil, fmt.Errorf("image has no pixels")
	}
	if int64(b.Dx())*int64(b.Dy()) > maxPersonPixels {
		return nil, nil, fmt.Errorf("image is too large (max %d megapixels)", maxPersonPixels/1_000_000)
	}
	oriented := orientImage(img, exifOrientation(data))
	fullImg := fitWithin(oriented, personFullSide, personFullSide)
	thumbImg := resizeWidth(oriented, personThumbWidth)

	var fbuf, tbuf bytes.Buffer
	if err := jpeg.Encode(&fbuf, fullImg, &jpeg.Options{Quality: personJPEGQuality}); err != nil {
		return nil, nil, err
	}
	if err := jpeg.Encode(&tbuf, thumbImg, &jpeg.Options{Quality: personThumbQuality}); err != nil {
		return nil, nil, err
	}
	return fbuf.Bytes(), tbuf.Bytes(), nil
}

// savePersonImageFiles writes both variants under dir and returns their file
// names. A timestamped name keeps browser caches honest after a re-upload.
func savePersonImageFiles(dir, id string, data []byte) (imageName, thumbName string, err error) {
	full, thumb, err := processPersonImage(data)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	stem := fmt.Sprintf("%s-%s", filepath.Base(id), strconvBase36(time.Now().UnixNano()))
	imageName = stem + ".jpg"
	thumbName = stem + "_t.jpg"
	if err := writeFileAtomic(filepath.Join(dir, thumbName), thumb); err != nil {
		return "", "", err
	}
	if err := writeFileAtomic(filepath.Join(dir, imageName), full); err != nil {
		os.Remove(filepath.Join(dir, thumbName))
		return "", "", err
	}
	return imageName, thumbName, nil
}

func strconvBase36(v int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 13)
	for v > 0 {
		buf = append([]byte{digits[v%36]}, buf...)
		v /= 36
	}
	return string(buf)
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// personImageFile resolves a stored file name inside dir. Names with any path
// component are rejected so a crafted value cannot escape the folder.
func personImageFile(dir, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || dir == "" {
		return "", false
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return filepath.Join(dir, name), true
}

// removePersonImages deletes both files, best effort.
func removePersonImages(dir string, p Person) {
	for _, n := range []string{p.Image, p.Thumb} {
		if path, ok := personImageFile(dir, n); ok {
			os.Remove(path)
		}
	}
}

// fitWithin scales an image down so neither side exceeds max. Smaller images
// are returned untouched.
func fitWithin(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	scale := float64(maxW) / float64(w)
	if s := float64(maxH) / float64(h); s < scale {
		scale = s
	}
	nw := maxInt(1, int(float64(w)*scale+0.5))
	nh := maxInt(1, int(float64(h)*scale+0.5))
	return areaResize(toRGBA(img), nw, nh)
}

// resizeWidth scales to an exact width, never enlarging.
func resizeWidth(img image.Image, width int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= width {
		return img
	}
	nh := maxInt(1, int(float64(h)*float64(width)/float64(w)+0.5))
	return areaResize(toRGBA(img), width, nh)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := y*dst.Stride + x*4
			dst.Pix[i] = uint8(r >> 8)
			dst.Pix[i+1] = uint8(g >> 8)
			dst.Pix[i+2] = uint8(bl >> 8)
			dst.Pix[i+3] = uint8(a >> 8)
		}
	}
	return dst
}

// areaResize is a box filter: each destination pixel averages its source
// rectangle. Good quality for the downscales used here, no external deps.
func areaResize(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if w == sw && h == sh {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0 := y * sh / h
		y1 := (y + 1) * sh / h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0 := x * sw / w
			x1 := (x + 1) * sw / w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, sa uint64
			var n uint64
			for yy := y0; yy < y1; yy++ {
				row := src.Pix[yy*src.Stride:]
				for xx := x0; xx < x1; xx++ {
					i := xx * 4
					sr += uint64(row[i])
					sg += uint64(row[i+1])
					sb += uint64(row[i+2])
					sa += uint64(row[i+3])
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := y*dst.Stride + x*4
			dst.Pix[i] = uint8(sr / n)
			dst.Pix[i+1] = uint8(sg / n)
			dst.Pix[i+2] = uint8(sb / n)
			dst.Pix[i+3] = uint8(sa / n)
		}
	}
	return dst
}

// orientImage applies an EXIF orientation (1-8). 0 or 1 means unchanged.
func orientImage(img image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := dy*dst.Stride + dx*4
			dst.Pix[i] = uint8(r >> 8)
			dst.Pix[i+1] = uint8(g >> 8)
			dst.Pix[i+2] = uint8(bl >> 8)
			dst.Pix[i+3] = uint8(a >> 8)
		}
	}
	return dst
}

// exifOrientation reads tag 0x0112 from the first APP1 segment of a JPEG.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA || marker == 0x00 {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if segLen < 2 {
			break
		}
		end := i + 2 + segLen
		if end > len(data) {
			break
		}
		if marker == 0xE1 {
			seg := data[i+4 : end]
			if len(seg) >= 14 && string(seg[:6]) == "Exif\x00\x00" {
				if o := tiffOrientation(seg[6:]); o != 0 {
					return o
				}
			}
		}
		i = end
	}
	return 0
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:4]) != 42 {
		return 0
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	count := int(bo.Uint16(t[off : off+2]))
	for i := 0; i < count; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:e+2]) != 0x0112 {
			continue
		}
		if bo.Uint16(t[e+2:e+4]) != 3 || bo.Uint32(t[e+4:e+8]) < 1 {
			return 0
		}
		if v := int(bo.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
			return v
		}
		return 0
	}
	return 0
}
