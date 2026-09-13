package fileguard

import (
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge reports a body that exceeded its ceiling. The bytes read before
// the ceiling are still valid; what follows was never read.
var ErrTooLarge = errors.New("fileguard: body exceeds size limit")

// LimitedReader wraps r so reading past max fails instead of truncating.
//
// io.LimitReader alone is not enough for a size gate: it reports io.EOF at the
// ceiling, so an oversized body is silently cut down to the limit and the
// caller stores a truncated file believing it complete. This reports ErrTooLarge
// instead, which is the difference between a limit and a trim.
//
// The ceiling applies to bytes that actually arrive, never to a declared
// Content-Length: the declaration is the sender's claim, and a size gate that
// trusts it is decoration.
func LimitedReader(r io.Reader, max int64) *CountingReader {
	return &CountingReader{r: io.LimitReader(r, max+1), max: max}
}

// CountingReader enforces a ceiling and reports how much was read, which
// callers need for quota accounting and for the error message.
type CountingReader struct {
	r   io.Reader
	max int64
	n   int64
}

// Read implements io.Reader, failing with ErrTooLarge once the ceiling is
// passed rather than reporting a short, clean EOF.
func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.max {
		return n, fmt.Errorf("%w: read %d bytes, limit %d", ErrTooLarge, c.n, c.max)
	}
	return n, err
}

// N returns the number of bytes read so far.
func (c *CountingReader) N() int64 { return c.n }

// ReadHeader reads the leading bytes needed for Sniff and returns them along
// with a reader that replays them, so the caller can sniff and then copy the
// whole body without seeking or buffering it twice.
//
// A body shorter than HeaderSize is not an error here: Sniff decides whether
// what arrived is enough to identify a container.
func ReadHeader(r io.Reader) ([]byte, io.Reader, error) {
	head := make([]byte, HeaderSize)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	head = head[:n]
	return head, io.MultiReader(bytesReader(head), r), nil
}

func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}
