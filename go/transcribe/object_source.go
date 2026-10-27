package transcribe

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ObjectFetcher is the slice of an object store an ObjectSource needs.
type ObjectFetcher interface {
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// ObjectSource localizes media held in an object store, so ffmpeg is handed a
// local path and never resolves a URL itself.
type ObjectSource struct {
	Storage ObjectFetcher
	Key     string
	// Ext names the temp file; some backends pick their demuxer from it.
	Ext string
	// MaxBytes bounds what is written to disk; 0 means no bound.
	MaxBytes int64
}

// Localize downloads the object to a temp file.
func (s ObjectSource) Localize(ctx context.Context) (string, func(), error) {
	if s.Storage == nil {
		return "", nil, fmt.Errorf("object source: no storage configured")
	}
	if s.Key == "" {
		return "", nil, fmt.Errorf("object source: no key")
	}

	rc, err := s.Storage.Download(ctx, s.Key)
	if err != nil {
		return "", nil, fmt.Errorf("download %s: %w", s.Key, err)
	}
	defer rc.Close()

	tmpDir, err := os.MkdirTemp("", "transcribe-object-*")
	if err != nil {
		return "", nil, fmt.Errorf("temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	path := filepath.Join(tmpDir, "source"+s.Ext)
	f, err := os.Create(path)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}

	var src io.Reader = rc
	if s.MaxBytes > 0 {
		src = io.LimitReader(rc, s.MaxBytes+1)
	}
	written, err := io.Copy(f, src)
	f.Close()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}
	if s.MaxBytes > 0 && written > s.MaxBytes {
		cleanup()
		return "", nil, fmt.Errorf("object %s exceeds %d bytes", s.Key, s.MaxBytes)
	}

	return path, cleanup, nil
}
