package reader

import (
	"encoding/binary"
	"io"
)

// binaryReader implements BinaryReader interface
type binaryReader struct {
	reader io.Reader
	pos    int64
}

// NewBinaryReader creates a new BinaryReader instance
func NewBinaryReader(reader io.Reader) BinaryReader {
	return &binaryReader{
		reader: reader,
		pos:    0,
	}
}

// ReadBytes reads n bytes from the current position
func (r *binaryReader) ReadBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	bytesRead, err := r.reader.Read(buf)
	if err != nil {
		return nil, err
	}
	if bytesRead < n {
		return buf[:bytesRead], io.ErrUnexpectedEOF
	}
	r.pos += int64(bytesRead)
	return buf, nil
}

// ReadUint32 reads a 32-bit unsigned integer
func (r *binaryReader) ReadUint32() (uint32, error) {
	bytes, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(bytes), nil
}

// ReadUint64 reads a 64-bit unsigned integer
func (r *binaryReader) ReadUint64() (uint64, error) {
	bytes, err := r.ReadBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(bytes), nil
}

// Skip skips n bytes from the current position
func (r *binaryReader) Skip(n int64) error {
	if seeker, ok := r.reader.(io.Seeker); ok {
		_, err := seeker.Seek(n, io.SeekCurrent)
		if err != nil {
			return err
		}
		r.pos += n
		return nil
	}
	
	// If reader doesn't support seeking, read and discard
	for n > 0 {
		toRead := int(n)
		if toRead > 4096 {
			toRead = 4096
		}
		_, err := r.ReadBytes(toRead)
		if err != nil {
			return err
		}
		n -= int64(toRead)
	}
	return nil
}

// Position returns the current position in the file
func (r *binaryReader) Position() int64 {
	return r.pos
}