package emoji

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// tables maps a four-byte table tag to the table's bytes within an sfnt
// file. golang.org/x/image/font/sfnt keeps its tables private, so the colour
// tables are read straight from the file.
type tables map[string][]byte

var errTruncated = errors.New("truncated table")

// readTables reads the table directory of a TrueType/OpenType file (not a
// collection).
func readTables(data []byte) (tables, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("emoji: font data too short")
	}
	switch tag := string(data[:4]); tag {
	case "ttcf":
		return nil, fmt.Errorf("emoji: font collections are not supported")
	case "\x00\x01\x00\x00", "OTTO", "true":
	default:
		return nil, fmt.Errorf("emoji: not an sfnt font (tag %q)", tag)
	}
	n := int(binary.BigEndian.Uint16(data[4:]))
	if len(data) < 12+16*n {
		return nil, fmt.Errorf("emoji: truncated table directory")
	}
	t := make(tables, n)
	for i := range n {
		rec := data[12+16*i:]
		off, length := uint64(binary.BigEndian.Uint32(rec[8:])), uint64(binary.BigEndian.Uint32(rec[12:]))
		if off+length > uint64(len(data)) {
			return nil, fmt.Errorf("emoji: table %q lies outside the file", rec[:4])
		}
		t[string(rec[:4])] = data[off : off+length]
	}
	return t, nil
}

// reader reads big-endian values at absolute offsets of a table. The first
// out-of-range read sets err and every read after it returns 0, so a parser
// can check err once at the end.
type reader struct {
	b   []byte
	err error
}

func (r *reader) u16(off int) int {
	if off < 0 || off+2 > len(r.b) {
		r.err = errTruncated
		return 0
	}
	return int(binary.BigEndian.Uint16(r.b[off:]))
}

func (r *reader) u32(off int) int {
	if off < 0 || off+4 > len(r.b) {
		r.err = errTruncated
		return 0
	}
	return int(binary.BigEndian.Uint32(r.b[off:]))
}

func (r *reader) tag(off int) string {
	if off < 0 || off+4 > len(r.b) {
		r.err = errTruncated
		return ""
	}
	return string(r.b[off : off+4])
}
