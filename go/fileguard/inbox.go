package fileguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// ErrDepositTooLarge reports a source that exceeded its ceiling mid-transfer.
var ErrDepositTooLarge = errors.New("fileguard: source exceeds the size limit")

// ErrSourceUnreachable reports a source that could not be fetched at all.
var ErrSourceUnreachable = errors.New("fileguard: source could not be fetched")

// ObjectWriter is the slice of object storage a deposit needs. Declared here
// rather than imported so this package keeps no dependencies: lib/objectstorage
// satisfies it as it stands.
type ObjectWriter interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
}

// Deposit describes what landed in the decontamination bucket.
type Deposit struct {
	Key       string
	Container Container
	Bytes     int64
}

// DepositOptions tunes one deposit.
type DepositOptions struct {
	// MaxBytes caps what is transferred. Enforced on bytes that arrive, never
	// on a declared Content-Length, which is the sender's claim.
	MaxBytes int64
	// Timeout bounds the whole fetch.
	Timeout time.Duration
	// RequireMedia rejects a container that is neither audio nor video before
	// anything is stored.
	RequireMedia bool
}

// DepositFromURL fetches a source and writes it to the decontamination bucket
// under key, returning what it found.
//
// This is the seam that lets a decoder run without a network: the fetch happens
// here, in Go, behind SafeHTTPClient — whose dialer validates the address after
// DNS resolution and so closes the rebinding window — and the decoder is then
// handed a key into one bucket it did not choose. A decoder that fetches the
// URL itself re-resolves the name, which no amount of up-front validation can
// cover.
//
// The bytes are sniffed before they are stored, so a source whose content
// contradicts what the pipeline expects never reaches the bucket at all.
func DepositFromURL(ctx context.Context, storage ObjectWriter, key, sourceURL string, opts DepositOptions) (Deposit, error) {
	if storage == nil {
		return Deposit{}, errors.New("fileguard: no object storage configured")
	}
	if err := ValidateFetchURL(sourceURL); err != nil {
		return Deposit{}, fmt.Errorf("%w: %v", ErrUnsafeURL, err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Deposit{}, fmt.Errorf("%w: %v", ErrSourceUnreachable, err)
	}
	resp, err := SafeHTTPClient(timeout).Do(req)
	if err != nil {
		return Deposit{}, fmt.Errorf("%w: %v", ErrSourceUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Deposit{}, fmt.Errorf("%w: status %d", ErrSourceUnreachable, resp.StatusCode)
	}

	return depositReader(ctx, storage, key, resp.Body, opts)
}

// DepositReader writes an already-open stream to the bucket under the same
// rules as DepositFromURL. Uploads take this path: their bytes are in the
// product's own storage, so there is nothing to fetch, only to sniff and move.
func DepositReader(ctx context.Context, storage ObjectWriter, key string, body io.Reader, opts DepositOptions) (Deposit, error) {
	if storage == nil {
		return Deposit{}, errors.New("fileguard: no object storage configured")
	}
	return depositReader(ctx, storage, key, body, opts)
}

func depositReader(ctx context.Context, storage ObjectWriter, key string, body io.Reader, opts DepositOptions) (Deposit, error) {
	head, rest, err := ReadHeader(body)
	if err != nil {
		return Deposit{}, fmt.Errorf("%w: %v", ErrSourceUnreachable, err)
	}

	container, err := Sniff(head)
	if err != nil {
		return Deposit{}, err
	}
	if opts.RequireMedia && !container.IsMedia() {
		return Deposit{}, fmt.Errorf("%w: %s", ErrNotMedia, container.MIME)
	}

	// rest replays the sniffed header, so the counter sees the whole object —
	// adding len(head) to its total would double every deposit's first bytes.
	// A ceiling is optional; counting is not, or Bytes would be a guess.
	max := opts.MaxBytes
	if max <= 0 {
		max = math.MaxInt64
	}
	counting := LimitedReader(rest, max)

	if err := storage.Upload(ctx, key, counting, container.MIME); err != nil {
		// A ceiling hit surfaces through Upload, which was reading the bounded
		// stream: report it as the size refusal it is rather than as a storage
		// failure the caller would retry.
		if errors.Is(err, ErrTooLarge) {
			return Deposit{}, fmt.Errorf("%w: limit %d bytes", ErrDepositTooLarge, opts.MaxBytes)
		}
		return Deposit{}, fmt.Errorf("deposit %s: %w", key, err)
	}

	return Deposit{Key: key, Container: container, Bytes: counting.N()}, nil
}
