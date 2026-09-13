package browser

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/draw"
)

const mosaicFileLimit = 6 * 1024 * 1024
const mosaicHeaderLimit = 64 * 1024
const mosaicMagic = "MFGOGRD1"

// mosaicCover stores only the identity needed to validate a library's primary
// artwork and its decoded pixels. It contains no session or navigation data.
type mosaicCover struct {
	key   imageKey
	image image.Image
}

type mosaicRecord struct {
	ID, Tag       string
	Width, Height int
}

// encodeMosaic writes a versioned manifest, tightly packed RGBA pixels, and a
// checksum. Incomplete tagged images are not persisted. Untagged slots are empty.
// CRC32 detects disk corruption without the ARM cost of hashing megabytes with
// a cryptographic digest. Cache files do not require cryptographic verification.
func encodeMosaic(covers []mosaicCover) ([]byte, error) {
	if len(covers) > 12 {
		return nil, errors.New("too many mosaic covers")
	}
	records := make([]mosaicRecord, len(covers))
	var pixels bytes.Buffer
	for i, cover := range covers {
		r := mosaicRecord{ID: cover.key.id, Tag: cover.key.tag}
		if cover.key.tag != "" {
			if cover.image == nil {
				return nil, errors.New("incomplete mosaic")
			}
			b := cover.image.Bounds()
			r.Width, r.Height = b.Dx(), b.Dy()
			if r.Width < 1 || r.Height < 1 || r.Width > 320 || r.Height > 360 {
				return nil, errors.New("invalid mosaic dimensions")
			}
			rgba := image.NewRGBA(image.Rect(0, 0, r.Width, r.Height))
			draw.Draw(rgba, rgba.Bounds(), cover.image, b.Min, draw.Src)
			pixels.Write(rgba.Pix)
		}
		records[i] = r
	}
	header, err := json.Marshal(records)
	if err != nil || len(header) > mosaicHeaderLimit {
		return nil, errors.New("invalid mosaic manifest")
	}
	data := make([]byte, 12, 12+len(header)+pixels.Len()+4)
	copy(data, mosaicMagic)
	binary.LittleEndian.PutUint32(data[8:], uint32(len(header)))
	data = append(data, header...)
	data = append(data, pixels.Bytes()...)
	return binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(data)), nil
}

// decodeMosaic validates the entire file before allocating decoded images.
// Limits and the checksum turn truncated, corrupt, or incompatible files into misses.
func decodeMosaic(data []byte) ([]mosaicCover, error) {
	invalid := errors.New("invalid mosaic cache")
	if len(data) < 12+4 || len(data) > mosaicFileLimit || string(data[:8]) != mosaicMagic {
		return nil, invalid
	}
	end := len(data) - 4
	if binary.LittleEndian.Uint32(data[end:]) != crc32.ChecksumIEEE(data[:end]) {
		return nil, invalid
	}
	size := int(binary.LittleEndian.Uint32(data[8:12]))
	if size > mosaicHeaderLimit || size > end-12 {
		return nil, invalid
	}
	var records []mosaicRecord
	if json.Unmarshal(data[12:12+size], &records) != nil || len(records) > 12 {
		return nil, invalid
	}
	offset := 12 + size
	for _, r := range records {
		if r.ID == "" || r.Width < 0 || r.Height < 0 || r.Width > 320 || r.Height > 360 {
			return nil, invalid
		}
		if (r.Tag == "" && (r.Width != 0 || r.Height != 0)) || (r.Tag != "" && (r.Width == 0 || r.Height == 0)) {
			return nil, invalid
		}
		offset += r.Width * r.Height * 4
	}
	if offset != end {
		return nil, invalid
	}
	covers := make([]mosaicCover, len(records))
	offset = 12 + size
	for i, r := range records {
		covers[i].key = imageKey{r.ID, "Primary", r.Tag}
		if r.Tag != "" {
			im := image.NewRGBA(image.Rect(0, 0, r.Width, r.Height))
			copy(im.Pix, data[offset:offset+len(im.Pix)])
			offset += len(im.Pix)
			covers[i].image = im
		}
	}
	return covers, nil
}
