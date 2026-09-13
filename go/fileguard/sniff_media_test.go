package fileguard

import "testing"

// Raw AAC (ADTS) starts with a 12-bit sync that also satisfies MP3's 11-bit
// sync test. Taking it for MP3 would reject a legitimate audio/aac upload as a
// mismatch, so ADTS must win.
func TestSniff_ADTSIsNotMP3(t *testing.T) {
	for _, head := range [][]byte{
		{0xFF, 0xF1, 0x50, 0x80}, // MPEG-4 AAC, no CRC
		{0xFF, 0xF9, 0x50, 0x80}, // MPEG-2 AAC
	} {
		got, err := Sniff(head)
		if err != nil {
			t.Fatalf("Sniff(%x): %v", head, err)
		}
		if got.MIME != "audio/aac" {
			t.Errorf("Sniff(%x).MIME = %q, want audio/aac", head, got.MIME)
		}
		if _, err := SniffMatching(head, "audio/aac"); err != nil {
			t.Errorf("declared audio/aac over ADTS bytes: %v", err)
		}
	}

	// A tagless MP3 frame keeps identifying as MP3.
	got, err := Sniff([]byte{0xFF, 0xFB, 0x90, 0x00})
	if err != nil || got.MIME != "audio/mpeg" {
		t.Errorf("MP3 frame sync: got %q, %v; want audio/mpeg", got.MIME, err)
	}
}

// 0xFF followed by a byte whose layer bits are 00 but whose sync is only 11
// bits long is neither a valid MP3 frame nor ADTS; it must not be guessed.
func TestSniff_ReservedLayerIsUnknown(t *testing.T) {
	if _, err := Sniff([]byte{0xFF, 0xE0, 0x00, 0x00}); err == nil {
		t.Error("reserved layer accepted as MP3")
	}
}

func TestContainer_IsMedia(t *testing.T) {
	media := []string{"audio/mpeg", "audio/wav", "video/mp4", "audio/ogg", "audio/flac", "video/webm", "audio/aac"}
	for _, m := range media {
		if !(Container{MIME: m}).IsMedia() {
			t.Errorf("%s: IsMedia() = false", m)
		}
	}
	if (Container{MIME: "application/pdf"}).IsMedia() {
		t.Error("application/pdf: IsMedia() = true")
	}

	// A PDF is a recognised container, so Sniff accepts it — which is exactly
	// why a media path has to ask IsMedia and not just check for an error.
	got, err := Sniff([]byte("%PDF-1.7\n"))
	if err != nil {
		t.Fatalf("Sniff(pdf): %v", err)
	}
	if got.IsMedia() {
		t.Error("PDF bytes reported as media")
	}
}
