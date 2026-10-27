package transcribe

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Segment is one transcribed unit. StartSeconds is the start offset of the
// chunk it came from. Start/End are absolute seconds: the provider's own timing
// when HasTiming is true, the chunk bounds otherwise (End is 0 for a
// single-shot file of unknown duration). Use HasTiming, not Start == 0, to tell
// the two apart.
type Segment struct {
	Text         string
	StartSeconds int
	Start        float64
	End          float64
	HasTiming    bool
}

// Result is the output of Transcribe.
type Result struct {
	Segments []Segment
	// DurationSeconds is the full length measured by ffprobe; 0 means unknown,
	// not empty.
	DurationSeconds float64
	// ProbeError is why DurationSeconds is unknown, nil when it was measured.
	ProbeError error
	// FailedChunks lists chunk indices that failed permanently; their text is
	// replaced by a marker.
	FailedChunks []int
	// Language is the first chunk's detected ISO 639-1 code, or "".
	Language string
}

// Options tunes a Transcribe call. Zero values take the defaults.
type Options struct {
	// Language is a hint for the backend; "" lets it detect.
	Language string
	// ChunkDuration in seconds; 0 means DefaultChunkDuration.
	ChunkDuration int
	// MaxSingleShotDuration in seconds: longer audio is chunked even when it
	// fits MaxFileSize. 0 means DefaultMaxSingleShotDuration.
	MaxSingleShotDuration int
	// MaxParallel bounds concurrent chunk transcriptions; 0 means 1.
	MaxParallel int
	// MaxDurationSeconds rejects longer audio with ErrTooLong before any
	// provider call; 0 means no cap. Audio whose duration could not be
	// measured passes through.
	MaxDurationSeconds float64
	// MaxBytesWhenUnmeasurable rejects files whose duration ffprobe could not
	// read with ErrUnmeasurable when they exceed it; 0 means no bound. A probed
	// file is never judged on its size.
	MaxBytesWhenUnmeasurable int64
	// RequireMeasurableAudio refuses, before any provider call, a file without
	// a decodable audio stream of finite duration (ErrNoAudioStream,
	// ErrUnboundedDuration, ErrUnreadableMedia).
	RequireMeasurableAudio bool
	// FFmpegPath and FFprobePath override the PATH lookup.
	FFmpegPath  string
	FFprobePath string
}

const (
	maxRetriesDefault     = 3
	maxRetriesRateLimited = 5
	retryBaseDelay        = 500 * time.Millisecond
	retryMaxDelay         = 30 * time.Second
	maxFailedChunkRatio   = 0.5
)

// DefaultMaxSingleShotDuration is the longest audio sent as one request, in
// seconds; some backends fail on long single files.
const DefaultMaxSingleShotDuration = 300

const chunkUnavailableMarker = "[transcription unavailable for segment %d]"

// ErrTooLong reports audio exceeding Options.MaxDurationSeconds.
var ErrTooLong = errors.New("audio exceeds the maximum duration")

// ErrUnmeasurable reports a file whose duration could not be read and whose
// size exceeds Options.MaxBytesWhenUnmeasurable.
var ErrUnmeasurable = errors.New("audio duration could not be measured and the file is too large to accept blindly")

// Transcriber runs the pipeline against one Provider. Safe for concurrent use.
type Transcriber struct {
	provider Provider
}

// New returns a Transcriber backed by provider.
func New(provider Provider) *Transcriber {
	return &Transcriber{provider: provider}
}

// Transcribe localizes src, routes it single-shot or chunked by size and
// duration, and returns the segments.
func (t *Transcriber) Transcribe(ctx context.Context, src Source, opts Options) (*Result, error) {
	if src == nil {
		return nil, fmt.Errorf("source is required")
	}
	opts = withDefaults(opts)

	path, cleanup, err := src.Localize(ctx)
	if err != nil {
		return nil, fmt.Errorf("localize source: %w", err)
	}
	defer cleanup()

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat audio file: %w", err)
	}
	fileSize := fi.Size()

	var duration float64
	var durErr error
	if opts.RequireMeasurableAudio {
		info, err := Probe(ctx, path, opts.FFprobePath)
		if err != nil {
			return nil, err
		}
		duration = info.DurationSeconds
	} else {
		duration, durErr = ProbeDuration(ctx, opts.FFprobePath, path)
		if durErr != nil {
			duration = 0
		}
	}

	if opts.MaxDurationSeconds > 0 && durErr == nil && duration > opts.MaxDurationSeconds {
		return nil, fmt.Errorf("%w: %.0fs > %.0fs", ErrTooLong, duration, opts.MaxDurationSeconds)
	}
	if durErr != nil && opts.MaxBytesWhenUnmeasurable > 0 && fileSize > opts.MaxBytesWhenUnmeasurable {
		return nil, fmt.Errorf("%w: %d bytes > %d (probe failed: %v)",
			ErrUnmeasurable, fileSize, opts.MaxBytesWhenUnmeasurable, durErr)
	}

	mustChunk := fileSize > t.provider.MaxFileSize() || (durErr == nil && int(duration) > opts.MaxSingleShotDuration)
	if mustChunk {
		res, err := t.transcribeChunked(ctx, path, opts)
		if err != nil {
			return nil, err
		}
		res.DurationSeconds = duration
		res.ProbeError = durErr
		return res, nil
	}

	res, err := t.transcribeFile(ctx, path, opts)
	if err != nil {
		return nil, err
	}
	return &Result{Segments: segmentsForChunk(res, 0, duration), DurationSeconds: duration, ProbeError: durErr, Language: res.Language}, nil
}

func withDefaults(o Options) Options {
	if o.ChunkDuration <= 0 {
		o.ChunkDuration = DefaultChunkDuration
	}
	if o.MaxParallel <= 0 {
		o.MaxParallel = 1
	}
	if o.MaxSingleShotDuration <= 0 {
		o.MaxSingleShotDuration = DefaultMaxSingleShotDuration
	}
	return o
}

func (t *Transcriber) transcribeFile(ctx context.Context, path string, opts Options) (*TranscribeResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}
	defer f.Close()

	return t.provider.Transcribe(ctx, TranscribeRequest{
		FileName: filepath.Base(path),
		File:     f,
		Language: opts.Language,
	})
}

func segmentsForChunk(res *TranscribeResult, chunkStartSeconds int, chunkEndSeconds float64) []Segment {
	off := float64(chunkStartSeconds)
	if len(res.Segments) == 0 {
		return []Segment{{
			Text:         res.Text,
			StartSeconds: chunkStartSeconds,
			Start:        off,
			End:          chunkEndSeconds,
		}}
	}
	out := make([]Segment, len(res.Segments))
	for i, s := range res.Segments {
		out[i] = Segment{
			Text:         s.Text,
			StartSeconds: chunkStartSeconds,
			Start:        off + s.Start,
			End:          off + s.End,
			HasTiming:    true,
		}
	}
	return out
}

func (t *Transcriber) transcribeChunked(ctx context.Context, path string, opts Options) (*Result, error) {
	split, err := SplitAudio(ctx, opts.FFmpegPath, path, opts.ChunkDuration)
	if err != nil {
		res, ferr := t.transcribeFile(ctx, path, opts)
		if ferr != nil {
			return nil, ferr
		}
		return &Result{Segments: segmentsForChunk(res, 0, 0), Language: res.Language}, nil
	}
	defer split.Cleanup()

	n := len(split.Chunks)
	results := make([]*TranscribeResult, n)
	errs := make([]error, n)

	sem := make(chan struct{}, opts.MaxParallel)
	done := make(chan struct{})
	var pending int
	for i, chunk := range split.Chunks {
		pending++
		go func(i int, chunkPath string) {
			defer func() { done <- struct{}{} }()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := t.transcribeChunkWithRetry(ctx, i, chunkPath, opts)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = res
		}(i, chunk.FilePath)
	}
	for ; pending > 0; pending-- {
		<-done
	}

	var failed []int
	for i, e := range errs {
		if e != nil {
			failed = append(failed, i)
		}
	}
	if len(failed) > 0 {
		ratio := float64(len(failed)) / float64(n)
		if len(failed) == n || ratio > maxFailedChunkRatio {
			return nil, fmt.Errorf("%d/%d chunks failed transcription: chunks %v", len(failed), n, failed)
		}
		for _, i := range failed {
			results[i] = &TranscribeResult{Text: fmt.Sprintf(chunkUnavailableMarker, i)}
		}
	}

	var segments []Segment
	var language string
	for i, res := range results {
		if language == "" && res != nil {
			language = res.Language
		}
		start := i * opts.ChunkDuration
		end := float64((i + 1) * opts.ChunkDuration)
		segments = append(segments, segmentsForChunk(res, start, end)...)
	}
	return &Result{Segments: segments, FailedChunks: failed, Language: language}, nil
}

func (t *Transcriber) transcribeChunkWithRetry(ctx context.Context, idx int, chunkPath string, opts Options) (*TranscribeResult, error) {
	fileName := filepath.Base(chunkPath)
	maxRetries := maxRetriesDefault
	var lastErr error

	for attempt := 1; ; attempt++ {
		f, err := os.Open(chunkPath)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: open: %w", idx, err)
		}
		res, err := t.provider.Transcribe(ctx, TranscribeRequest{
			FileName: fileName,
			File:     f,
			Language: opts.Language,
		})
		f.Close()

		if err == nil {
			return res, nil
		}
		lastErr = err

		var rle *RateLimitError
		isRateLimited := errors.As(err, &rle)
		if isRateLimited && maxRetries < maxRetriesRateLimited {
			maxRetries = maxRetriesRateLimited
		}
		if attempt >= maxRetries {
			break
		}

		delay := backoffDelay(attempt)
		if isRateLimited && rle.RetryAfter > 0 {
			delay = rle.RetryAfter
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, fmt.Errorf("chunk %d: %w", idx, err)
		}
	}
	return nil, fmt.Errorf("chunk %d: all %d attempts failed: %w", idx, maxRetries, lastErr)
}

func backoffDelay(attempt int) time.Duration {
	exp := retryBaseDelay << (attempt - 1)
	if exp > retryMaxDelay || exp <= 0 {
		exp = retryMaxDelay
	}
	return time.Duration(rand.Int63n(int64(exp) + 1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}

// JoinText concatenates segment texts in order, separated by a space.
func JoinText(segs []Segment) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.Text
	}
	return strings.Join(parts, " ")
}
