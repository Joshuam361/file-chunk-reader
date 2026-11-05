package filechunkreader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeTemp creates a temp file with the given contents and returns its path.
func writeTemp(t *testing.T, contents []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestNewRejectsNonPositiveChunkSize(t *testing.T) {
	path := writeTemp(t, []byte("abc"))
	for _, size := range []int{0, -1, -1024} {
		_, err := New(path, size)
		if !errors.Is(err, ErrInvalidChunkSize) {
			t.Fatalf("New(size=%d) error = %v, want ErrInvalidChunkSize", size, err)
		}
	}
}

func TestNewMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "does-not-exist"), 4)
	if err == nil {
		t.Fatal("New on missing file returned nil error")
	}
}

func TestReadWholeFileInChunks(t *testing.T) {
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	path := writeTemp(t, data)
	r, err := New(path, 5)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	var got []byte
	for off := int64(0); ; off += int64(r.ChunkSize()) {
		chunk, err := r.ReadChunk(off)
		if err != nil {
			t.Fatalf("ReadChunk(%d): %v", off, err)
		}
		if len(chunk) == 0 {
			break
		}
		// Copy because the buffer is reused on the next call.
		got = append(got, chunk...)
	}
	if string(got) != string(data) {
		t.Fatalf("assembled = %q, want %q", got, data)
	}
}

func TestReadChunkAtArbitraryOffset(t *testing.T) {
	path := writeTemp(t, []byte("0123456789"))
	r, err := New(path, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	cases := []struct {
		offset int64
		want   string
	}{
		{0, "012"},
		{1, "123"},
		{3, "345"},
		{7, "789"},
		{8, "89"}, // partial tail
		{9, "9"},  // one byte left
		{10, ""},  // exactly at EOF
		{11, ""},  // past EOF
		{100, ""}, // far past EOF
	}
	for _, tc := range cases {
		got, err := r.ReadChunk(tc.offset)
		if err != nil {
			t.Fatalf("ReadChunk(%d): %v", tc.offset, err)
		}
		if string(got) != tc.want {
			t.Fatalf("ReadChunk(%d) = %q, want %q", tc.offset, got, tc.want)
		}
	}
}

func TestReadChunkNegativeOffset(t *testing.T) {
	path := writeTemp(t, []byte("abc"))
	r, err := New(path, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	if _, err := r.ReadChunk(-1); err == nil {
		t.Fatal("ReadChunk(-1) returned nil error")
	}
}

func TestChunkBufferIsReused(t *testing.T) {
	// The zero-copy contract means the same backing array is handed back
	// every call. Asserting this pins the behaviour the README documents.
	path := writeTemp(t, []byte("abcdef"))
	r, err := New(path, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	first, err := r.ReadChunk(0)
	if err != nil {
		t.Fatalf("ReadChunk(0): %v", err)
	}
	if string(first) != "abc" {
		t.Fatalf("first = %q, want %q", first, "abc")
	}

	second, err := r.ReadChunk(3)
	if err != nil {
		t.Fatalf("ReadChunk(3): %v", err)
	}
	if string(second) != "def" {
		t.Fatalf("second = %q, want %q", second, "def")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected the same backing array to be reused")
	}
}

func TestEmptyFile(t *testing.T) {
	path := writeTemp(t, []byte{})
	r, err := New(path, 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	size, err := r.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 0 {
		t.Fatalf("Size = %d, want 0", size)
	}

	chunk, err := r.ReadChunk(0)
	if err != nil {
		t.Fatalf("ReadChunk(0): %v", err)
	}
	if len(chunk) != 0 {
		t.Fatalf("ReadChunk(0) len = %d, want 0", len(chunk))
	}
}

func TestSizeAndChunkSize(t *testing.T) {
	data := []byte("0123456789")
	path := writeTemp(t, data)
	r, err := New(path, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	if r.ChunkSize() != 4 {
		t.Fatalf("ChunkSize = %d, want 4", r.ChunkSize())
	}
	size, err := r.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != int64(len(data)) {
		t.Fatalf("Size = %d, want %d", size, len(data))
	}
}

func TestCloseIsIdempotentAndBlocksReads(t *testing.T) {
	path := writeTemp(t, []byte("abcdef"))
	r, err := New(path, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := r.ReadChunk(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("ReadChunk after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.Size(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Size after Close error = %v, want ErrClosed", err)
	}
}

func TestChunkLargerThanFile(t *testing.T) {
	path := writeTemp(t, []byte("hi"))
	r, err := New(path, 64)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	got, err := r.ReadChunk(0)
	if err != nil {
		t.Fatalf("ReadChunk(0): %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("ReadChunk(0) = %q, want %q", got, "hi")
	}
}

func TestExactMultipleOfChunkSize(t *testing.T) {
	// The last full chunk and the read at EOF must both behave: the former
	// returns chunkSize bytes, the latter an empty slice with nil error.
	path := writeTemp(t, []byte("abcdef"))
	r, err := New(path, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	last, err := r.ReadChunk(3)
	if err != nil {
		t.Fatalf("ReadChunk(3): %v", err)
	}
	if string(last) != "def" {
		t.Fatalf("ReadChunk(3) = %q, want %q", last, "def")
	}

	eof, err := r.ReadChunk(6)
	if err != nil {
		t.Fatalf("ReadChunk(6): %v", err)
	}
	if len(eof) != 0 {
		t.Fatalf("ReadChunk(6) len = %d, want 0", len(eof))
	}
}
