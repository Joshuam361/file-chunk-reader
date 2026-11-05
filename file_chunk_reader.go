// Package filechunkreader reads fixed-size chunks from files at arbitrary
// offsets without loading the whole file into memory.
//
// The central type is Reader, which wraps an *os.File and hands out byte
// slices backed by a single reused buffer. Callers that need to keep the
// bytes past the next ReadChunk call must copy them; the zero-copy default
// is what keeps memory bounded regardless of file size.
package filechunkreader

import (
	"errors"
	"io"
	"os"
)

// ErrClosed is returned by ReadChunk after Close has been called.
var ErrClosed = errors.New("filechunkreader: reader is closed")

// ErrInvalidChunkSize is returned by New when chunkSize is not positive.
var ErrInvalidChunkSize = errors.New("filechunkreader: chunk size must be positive")

// Reader reads fixed-size chunks from a file at arbitrary offsets.
//
// The buffer returned by ReadChunk is owned by the Reader and is reused on
// every call. This is deliberate: it means a Reader of any size allocates
// exactly one chunk-sized buffer, so reading a 100 GB file costs the same
// as reading a 100 MB one. The cost is that callers must copy the bytes if
// they want to retain them across calls.
type Reader struct {
	f         *os.File
	chunkSize int
	buf       []byte
	closed    bool
}

// New opens path and returns a Reader that hands out chunks of chunkSize
// bytes. The file is opened read-only.
//
// chunkSize must be positive; New returns ErrInvalidChunkSize otherwise.
func New(path string, chunkSize int) (*Reader, error) {
	if chunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Reader{
		f:         f,
		chunkSize: chunkSize,
		buf:       make([]byte, chunkSize),
	}, nil
}

// Size reports the total size of the underlying file in bytes.
func (r *Reader) Size() (int64, error) {
	if r.closed {
		return 0, ErrClosed
	}
	info, err := r.f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ChunkSize reports the configured chunk size.
func (r *Reader) ChunkSize() int {
	return r.chunkSize
}

// ReadChunk reads up to chunkSize bytes starting at offset.
//
// The returned slice aliases the Reader's internal buffer and is only valid
// until the next call to ReadChunk or Close. A short slice is returned when
// the offset is within the last partial chunk of the file; at or past EOF
// the result is a zero-length slice and a nil error, which lets callers
// drive a loop without special-casing the tail.
//
// A negative offset returns an error from the underlying Seek.
func (r *Reader) ReadChunk(offset int64) ([]byte, error) {
	if r.closed {
		return nil, ErrClosed
	}
	if offset < 0 {
		return nil, errors.New("filechunkreader: negative offset")
	}
	if _, err := r.f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	n, err := io.ReadFull(r.f, r.buf)
	switch {
	case err == nil:
		return r.buf, nil
	case err == io.ErrUnexpectedEOF || err == io.EOF:
		// Tail of the file: return the partial chunk. io.ReadFull reports
		// EOF only when n == 0, which is the at-or-past-EOF case.
		return r.buf[:n], nil
	default:
		return nil, err
	}
}

// Close releases the underlying file handle. ReadChunk after Close returns
// ErrClosed. Close is idempotent.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.f.Close()
}
