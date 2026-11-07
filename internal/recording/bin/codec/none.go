package codec

// NoneCodec stores data unchanged. The dictionary is ignored.
type NoneCodec struct{}

// ID returns None.
func (NoneCodec) ID() ID { return None }

// Compress appends src to dst.
func (NoneCodec) Compress(dst, src, _ []byte) ([]byte, error) {
	return append(dst, src...), nil
}

// Decompress appends src to dst, or returns ErrTooLarge if len(src) > maxLen.
func (NoneCodec) Decompress(dst, src, _ []byte, maxLen int) ([]byte, error) {
	if maxLen < 0 {
		return dst, errNegativeMaxLen
	}
	if len(src) > maxLen {
		return dst, ErrTooLarge
	}
	return append(dst, src...), nil
}
