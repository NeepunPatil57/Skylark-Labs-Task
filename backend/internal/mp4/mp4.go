// Package mp4 splits a fragmented MP4 byte stream (as written by FFmpeg with
// -movflags frag_keyframe+empty_moov) into an init segment and media segments.
package mp4

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// maxBoxSize guards against corrupt headers making us allocate huge buffers.
const maxBoxSize = 64 << 20

// Box is one top-level MP4 box, header included.
type Box struct {
	Type string
	Data []byte
}

// ReadBox reads a single box from r.
func ReadBox(r io.Reader) (Box, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Box{}, err
	}
	size := uint64(binary.BigEndian.Uint32(hdr[:4]))
	typ := string(hdr[4:8])
	headerLen := uint64(8)
	var ext [8]byte
	if size == 1 {
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Box{}, unexpected(err)
		}
		size = binary.BigEndian.Uint64(ext[:])
		headerLen = 16
	}
	if size < headerLen {
		// size 0 ("box extends to end of file") never appears in a live stream.
		return Box{}, fmt.Errorf("mp4: invalid size %d for box %q", size, typ)
	}
	if size > maxBoxSize {
		return Box{}, fmt.Errorf("mp4: box %q too large (%d bytes)", typ, size)
	}

	data := make([]byte, size)
	copy(data, hdr[:])
	if headerLen == 16 {
		copy(data[8:], ext[:])
	}
	if _, err := io.ReadFull(r, data[headerLen:]); err != nil {
		return Box{}, unexpected(err)
	}
	return Box{Type: typ, Data: data}, nil
}

// Segment is either the init segment (ftyp+moov) or one media fragment
// (moof+mdat, plus any boxes FFmpeg emits between them).
type Segment struct {
	Init  bool
	Codec string // set on init segments only, e.g. "avc1.4D401F"
	Data  []byte
}

// Segmenter groups top-level boxes into playable segments.
type Segmenter struct {
	r   *bufio.Reader
	buf []byte
}

func NewSegmenter(r io.Reader) *Segmenter {
	return &Segmenter{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next complete segment. It returns io.EOF when the stream
// ends cleanly between segments.
func (s *Segmenter) Next() (Segment, error) {
	for {
		box, err := ReadBox(s.r)
		if err != nil {
			if errors.Is(err, io.EOF) && len(s.buf) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return Segment{}, err
		}

		switch box.Type {
		case "moov":
			data := append(s.buf, box.Data...)
			s.buf = nil
			return Segment{Init: true, Codec: Codec(box.Data), Data: data}, nil
		case "mdat":
			data := append(s.buf, box.Data...)
			s.buf = nil
			return Segment{Data: data}, nil
		default:
			// ftyp, moof, styp, sidx...: hold until the closing moov/mdat.
			s.buf = append(s.buf, box.Data...)
		}
	}
}

// Codec returns the RFC 6381 codec string of the first video track in a moov
// box (e.g. "avc1.4D401F"), the bare sample entry type for codecs it doesn't
// decode in detail (e.g. "hev1"), or "" if none is found.
func Codec(moov []byte) string {
	path := []string{"trak", "mdia", "minf", "stbl", "stsd"}
	return findCodec(children(moov[8:]), path)
}

func findCodec(boxes []Box, path []string) string {
	for _, b := range boxes {
		if b.Type != path[0] {
			continue
		}
		body := b.Data[8:]
		if len(path) > 1 {
			if c := findCodec(children(body), path[1:]); c != "" {
				return c
			}
			continue
		}
		// stsd: version/flags (4) + entry count (4), then sample entries.
		if len(body) < 8 {
			return ""
		}
		for _, entry := range children(body[8:]) {
			if c := sampleEntryCodec(entry); c != "" {
				return c
			}
		}
	}
	return ""
}

func sampleEntryCodec(entry Box) string {
	switch entry.Type {
	case "avc1", "avc3":
		// Visual sample entry: 8 header + 78 fixed bytes, then child boxes.
		if len(entry.Data) < 86 {
			return entry.Type
		}
		for _, c := range children(entry.Data[86:]) {
			// avcC: configurationVersion, profile, compatibility, level.
			if c.Type == "avcC" && len(c.Data) >= 12 {
				return fmt.Sprintf("%s.%02X%02X%02X", entry.Type, c.Data[9], c.Data[10], c.Data[11])
			}
		}
		return entry.Type
	case "hev1", "hvc1", "av01", "vp09":
		return entry.Type
	}
	return ""
}

// children parses the boxes packed in b, stopping at the first malformed one.
func children(b []byte) []Box {
	var out []Box
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b[:4]))
		if size < 8 || size > len(b) {
			break
		}
		out = append(out, Box{Type: string(b[4:8]), Data: b[:size]})
		b = b[size:]
	}
	return out
}

func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
