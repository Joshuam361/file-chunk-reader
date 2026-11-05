# file-chunk-reader

Reads fixed-size chunks from a file at arbitrary byte offsets, reusing a single buffer so memory stays bounded no matter how large the file is.

```go
package main

import (
	"fmt"
	"log"

	filechunkreader "example.com/file-chunk-reader"
)

func main() {
	r, err := filechunkreader.New("big.bin", 1<<20) // 1 MiB chunks
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	// Walk the file from the start.
	for off := int64(0); ; off += int64(r.ChunkSize()) {
		chunk, err := r.ReadChunk(off)
		if err != nil {
			log.Fatal(err)
		}
		if len(chunk) == 0 {
			break // at or past EOF
		}
		fmt.Println(off, len(chunk))
	}

	// Or read a single window somewhere in the middle.
	window, err := r.ReadChunk(4096)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(window))
}
```

## Why this exists

Reading a large file with `os.ReadFile` or `io.ReadAll` allocates a buffer the size of the file. For multi-gigabyte inputs that is either impossible or rude to the rest of the process. `filechunkreader.Reader` allocates exactly one chunk-sized buffer at construction time and reuses it for every read, so peak memory is a function of the chunk size you choose, not the file size.

The trade-off is deliberate: `ReadChunk` returns a slice that aliases the Reader's internal buffer. It is valid only until the next call to `ReadChunk` or `Close`. If you need to keep the bytes, copy them. This is the zero-copy contract, and it is what makes the bounded-memory promise real rather than nominal.

The other decision worth stating plainly: an offset at or past EOF returns an empty slice and a nil error, not `io.EOF`. That means a `for` loop can advance by `ChunkSize()` and stop on `len(chunk) == 0` without a terminal special case. A partial final chunk is returned short, also with a nil error.

## API

- `New(path string, chunkSize int) (*Reader, error)` — opens the file read-only. Returns `ErrInvalidChunkSize` if `chunkSize <= 0`.
- `(*Reader).ReadChunk(offset int64) ([]byte, error)` — reads up to `chunkSize` bytes at `offset`. The result aliases the internal buffer. A negative offset returns an error; at or past EOF the result is empty with a nil error.
- `(*Reader).Size() (int64, error)` — total file size in bytes.
- `(*Reader).ChunkSize() int` — the configured chunk size.
- `(*Reader).Close() error` — releases the file handle. Idempotent. Subsequent `ReadChunk` and `Size` calls return `ErrClosed`.
- `ErrInvalidChunkSize`, `ErrClosed` — sentinel errors.

## The awkward edge

The returned slice is only valid until the next call. This bites in the obvious way:

```go
first, _ := r.ReadChunk(0)
second, _ := r.ReadChunk(3)
// first now contains the same bytes as second.
```

Copy with `append([]byte(nil), chunk...)` if you need to retain the data. The test suite pins this reuse behaviour so it cannot change silently.
