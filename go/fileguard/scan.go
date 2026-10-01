package fileguard

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ErrScannerUnavailable is clamd not answering: the bytes were not scanned,
// which a caller must not read as clean.
var ErrScannerUnavailable = errors.New("fileguard: virus scanner unavailable")

// ErrScanTooLarge is clamd refusing a stream past its StreamMaxLength: the
// bytes were not scanned either.
var ErrScanTooLarge = errors.New("fileguard: body exceeds the scanner's stream limit")

// Verdict is clamd's answer on one stream.
type Verdict struct {
	Infected bool
	// Signature names what was found, e.g. "Eicar-Test-Signature".
	Signature string
}

type ScannerOptions struct {
	// Timeout bounds one scan, connection included; zero means 2 minutes.
	Timeout time.Duration
	// ChunkSize is the INSTREAM chunk; zero means 64 KiB.
	ChunkSize int
}

// Scanner talks to clamd over its INSTREAM command, so the bytes travel on the
// connection and never through a filesystem the two would have to share.
type Scanner struct {
	network, address string
	opts             ScannerOptions
}

// NewScanner takes clamd's address as "tcp://host:port" or "unix:///path".
func NewScanner(address string, opts ScannerOptions) (*Scanner, error) {
	network, rest, ok := strings.Cut(address, "://")
	if !ok || (network != "tcp" && network != "unix") || rest == "" {
		return nil, fmt.Errorf("fileguard: clamd address %q is not tcp://host:port or unix:///path", address)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 64 << 10
	}
	return &Scanner{network: network, address: rest, opts: opts}, nil
}

// Scan streams r to clamd. An error means the bytes were not scanned — the
// scanner was unreachable, refused the size, or r failed — and the caller
// decides what an unscanned file may do; only a nil error carries a verdict.
func (s *Scanner) Scan(ctx context.Context, r io.Reader) (Verdict, error) {
	conn, err := s.dial(ctx)
	if err != nil {
		return Verdict{}, err
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return Verdict{}, fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	buf := make([]byte, 4+s.opts.ChunkSize)
	for {
		n, readErr := r.Read(buf[4:])
		if n > 0 {
			binary.BigEndian.PutUint32(buf[:4], uint32(n))
			if _, err := conn.Write(buf[:4+n]); err != nil {
				// clamd closes the connection once a stream passes its limit;
				// its reply says so, the failed write does not.
				return s.reply(conn, err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return Verdict{}, readErr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return s.reply(conn, err)
	}
	return s.reply(conn, nil)
}

// Ping reports whether clamd answers, for readiness checks.
func (s *Scanner) Ping(ctx context.Context) error {
	conn, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "zPING\x00"); err != nil {
		return fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	line, err := readReply(conn)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	if line != "PONG" {
		return fmt.Errorf("%w: unexpected answer %q", ErrScannerUnavailable, line)
	}
	return nil
}

func (s *Scanner) dial(ctx context.Context) (net.Conn, error) {
	deadline := time.Now().Add(s.opts.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, s.network, s.address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	_ = conn.SetDeadline(deadline)
	return conn, nil
}

func (s *Scanner) reply(conn net.Conn, writeErr error) (Verdict, error) {
	line, err := readReply(conn)
	if err != nil {
		if writeErr != nil {
			return Verdict{}, fmt.Errorf("%w: %v", ErrScannerUnavailable, writeErr)
		}
		return Verdict{}, fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	return parseReply(line)
}

func readReply(conn net.Conn) (string, error) {
	line, err := bufio.NewReader(conn).ReadString(0)
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(strings.TrimSuffix(line, "\x00")), nil
}

// parseReply reads "stream: OK", "stream: <signature> FOUND" and the errors
// clamd ends with " ERROR".
func parseReply(line string) (Verdict, error) {
	switch {
	case strings.HasSuffix(line, " FOUND"):
		signature := strings.TrimSuffix(line, " FOUND")
		if _, after, ok := strings.Cut(signature, ": "); ok {
			signature = after
		}
		return Verdict{Infected: true, Signature: signature}, nil
	case strings.HasSuffix(line, ": OK") || line == "OK":
		return Verdict{}, nil
	case strings.Contains(line, "size limit exceeded"):
		return Verdict{}, ErrScanTooLarge
	}
	return Verdict{}, fmt.Errorf("%w: clamd answered %q", ErrScannerUnavailable, line)
}
