package audioreader

import (
	"io"
	"log/slog"
)

// testLogger discards output — tests assert on behaviour, not log lines.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
