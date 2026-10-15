package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// box builds an MP4 box with a 32-bit size header.
func box(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], typ)
	return append(out, body...)
}

// moov builds a minimal moov whose single track uses the given sample entry.
func moov(entry []byte) []byte {
	stsd := box("stsd", make([]byte, 8), entry) // version/flags + entry count
	return box("moov", box("trak", box("mdia", box("minf", box("stbl", stsd)))))
}

func avcEntry(typ string, profile, compat, level byte) []byte {
	avcC := box("avcC", []byte{1, profile, compat, level, 0xff})
	return box(typ, make([]byte, 78), avcC)
}

func TestCodec(t *testing.T) {
	tests := []struct {
		name  string
		entry []byte
		want  string
	}{
		{"h264 high", avcEntry("avc1", 0x64, 0x00, 0x1f), "avc1.64001F"},
		{"h264 main avc3", avcEntry("avc3", 0x4d, 0x40, 0x1e), "avc3.4D401E"},
		{"h264 without avcC", box("avc1", make([]byte, 78)), "avc1"},
		{"h265", box("hvc1", make([]byte, 78)), "hvc1"},
		{"h265 hev1", box("hev1", make([]byte, 78)), "hev1"},
		{"unknown entry", box("mp4a", make([]byte, 20)), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Codec(moov(tt.entry)); got != tt.want {
				t.Errorf("Codec() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodecMalformed(t *testing.T) {
	// Truncated or garbage moov contents must not panic.
	for _, b := range [][]byte{
		box("moov"),
		box("moov", []byte{0, 0, 0, 3, 'x'}),
		box("moov", box("trak", []byte{0xff, 0xff, 0xff, 0xff, 'm', 'd', 'i', 'a'})),
		box("moov", box("trak", box("mdia", box("minf", box("stbl", box("stsd", []byte{0, 0})))))),
	} {
		if got := Codec(b); got != "" {
			t.Errorf("Codec(%x) = %q, want empty", b, got)
		}
	}
}

func TestSegmenter(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	mv := moov(avcEntry("avc1", 0x4d, 0x40, 0x1f))
	moof1, mdat1 := box("moof", []byte{1}), box("mdat", []byte("frame-1"))
	moof2, mdat2 := box("moof", []byte{2}), box("mdat", []byte("frame-2"))
	stream := bytes.Join([][]byte{ftyp, mv, moof1, mdat1, moof2, mdat2}, nil)

	// Feed one byte at a time: segments must not depend on read boundaries.
	s := NewSegmenter(&oneByteReader{r: bytes.NewReader(stream)})

	init, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !init.Init || init.Codec != "avc1.4D401F" || !bytes.Equal(init.Data, append(ftyp, mv...)) {
		t.Errorf("init segment = {Init:%v Codec:%q len:%d}", init.Init, init.Codec, len(init.Data))
	}
	for i, want := range [][]byte{append(moof1, mdat1...), append(moof2, mdat2...)} {
		seg, err := s.Next()
		if err != nil {
			t.Fatalf("segment %d: %v", i, err)
		}
		if seg.Init || !bytes.Equal(seg.Data, want) {
			t.Errorf("segment %d = %x, want %x", i, seg.Data, want)
		}
	}
	if _, err := s.Next(); err != io.EOF {
		t.Errorf("after last segment: err = %v, want io.EOF", err)
	}
}

func TestSegmenterTruncated(t *testing.T) {
	stream := append(box("moof", []byte{1}), box("mdat", []byte("frame"))[:10]...)
	_, err := NewSegmenter(bytes.NewReader(stream)).Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestSegmenterEOFMidSegment(t *testing.T) {
	// A moof with no mdat: the stream ended inside a segment.
	_, err := NewSegmenter(bytes.NewReader(box("moof", []byte{1}))).Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadBoxLargeSize(t *testing.T) {
	// size == 1 means a 64-bit size follows the type.
	hdr := []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 0, 20}
	data := append(hdr, []byte("abcd")...)
	b, err := ReadBox(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b.Type != "mdat" || !bytes.Equal(b.Data, data) {
		t.Errorf("got %q %x", b.Type, b.Data)
	}
}

func TestReadBoxInvalid(t *testing.T) {
	tests := map[string][]byte{
		"size smaller than header": {0, 0, 0, 4, 'm', 'o', 'o', 'f'},
		"size zero":                {0, 0, 0, 0, 'm', 'o', 'o', 'f'},
		"too large":                {0x7f, 0xff, 0xff, 0xff, 'm', 'd', 'a', 't'},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadBox(bytes.NewReader(data)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}
