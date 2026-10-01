package fileguard

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSniff_IdentifiesContainers(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"mp3 with id3 tag", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), "audio/mpeg"},
		{"mp3 tagless frame sync", []byte{0xFF, 0xFB, 0x90, 0x00}, "audio/mpeg"},
		{"wav", []byte("RIFF\x24\x08\x00\x00WAVEfmt "), "audio/wav"},
		{"mp4 ftyp box", []byte("\x00\x00\x00\x20ftypisom\x00\x00\x02\x00"), "video/mp4"},
		{"ogg", []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00"), "audio/ogg"},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), "audio/flac"},
		{"matroska ebml", []byte("\x1aE\xdf\xa3\x01\x00\x00\x00"), "video/webm"},
		{"pdf", []byte("%PDF-1.7\n%\xc7\xec"), "application/pdf"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Sniff(tc.head)
			if err != nil {
				t.Fatalf("Sniff: %v", err)
			}
			if got.MIME != tc.want {
				t.Errorf("MIME = %q, want %q", got.MIME, tc.want)
			}
			if !strings.HasPrefix(got.Ext, ".") {
				t.Errorf("Ext = %q, want a leading dot", got.Ext)
			}
		})
	}
}

// Unrecognised bytes must fail rather than default to a plausible container.
// The sniffer this replaces fell through to .m4a, which is why nothing could
// ever be rejected by it.
func TestSniff_RejectsUnknownAndTruncated(t *testing.T) {
	for _, head := range [][]byte{
		[]byte("not a media file at all"),
		[]byte("<!DOCTYPE html><html><body>"),
		[]byte("MZ\x90\x00"), // PE executable
		[]byte("ID"),         // truncated ID3
		[]byte("RIFF"),       // RIFF without the WAVE tag
		{},
	} {
		if _, err := Sniff(head); !errors.Is(err, ErrUnknownContainer) {
			t.Errorf("Sniff(%q) error = %v, want ErrUnknownContainer", head, err)
		}
	}
}

// A RIFF container that is not WAVE (AVI, for instance) shares the first four
// bytes with WAV, so the WAVE tag at offset 8 is what separates them.
func TestSniff_RIFFRequiresWAVETag(t *testing.T) {
	avi := []byte("RIFF\x24\x08\x00\x00AVI LIST")
	if _, err := Sniff(avi); !errors.Is(err, ErrUnknownContainer) {
		t.Errorf("RIFF/AVI error = %v, want ErrUnknownContainer", err)
	}
}

func TestSniffMatching_AcceptsSpellingsOfOneContainer(t *testing.T) {
	ftyp := []byte("\x00\x00\x00\x20ftypM4A \x00\x00\x02\x00")
	// Clients spell the ftyp box many ways; all describe the same bytes, and the
	// pipeline demuxes audio out of a video container regardless.
	for _, declared := range []string{"audio/mp4", "audio/x-m4a", "audio/mp4a-latm", "video/mp4", "video/quicktime"} {
		if _, err := SniffMatching(ftyp, declared); err != nil {
			t.Errorf("declared %q: %v", declared, err)
		}
	}

	// Parameters and casing must not matter, per RFC 9110.
	if _, err := SniffMatching([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00"), "Audio/OGG; codecs=opus"); err != nil {
		t.Errorf("parameterised declaration: %v", err)
	}
}

func TestSniffMatching_RejectsContradiction(t *testing.T) {
	// The classic case: an allowlisted audio type declared over something else.
	pdf := []byte("%PDF-1.7\n%\xc7\xec")
	_, err := SniffMatching(pdf, "audio/mpeg")
	if !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("error = %v, want ErrTypeMismatch", err)
	}
	if strings.Contains(err.Error(), "%PDF") {
		t.Error("error message echoes raw file bytes")
	}
}

// A size gate must fail, not trim: io.LimitReader alone reports EOF at the
// ceiling, so an oversized body would be stored truncated but believed whole.
func TestLimitedReader_FailsInsteadOfTruncating(t *testing.T) {
	body := strings.NewReader(strings.Repeat("a", 5000))
	r := LimitedReader(body, 1024)

	n, err := io.Copy(io.Discard, r)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	if n > 1024+1 {
		t.Errorf("copied %d bytes past the limit", n)
	}
}

func TestLimitedReader_PassesBodyAtLimit(t *testing.T) {
	body := strings.NewReader(strings.Repeat("a", 1024))
	r := LimitedReader(body, 1024)

	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if n != 1024 {
		t.Errorf("copied %d bytes, want 1024", n)
	}
	if r.N() != 1024 {
		t.Errorf("N() = %d, want 1024", r.N())
	}
}

func TestReadHeader_ReplaysConsumedBytes(t *testing.T) {
	const payload = "OggS\x00\x02" + "the rest of the body follows here"
	head, body, err := ReadHeader(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if string(head) != payload {
		t.Errorf("head = %q, want the whole short body", head)
	}

	// The bytes used for sniffing must still reach the caller, or the stored
	// file loses its first 512 bytes.
	all, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(all) != payload {
		t.Errorf("replayed %q, want %q", all, payload)
	}
}

func TestReadHeader_SniffsThenReplaysLongBody(t *testing.T) {
	payload := "fLaC\x00\x00\x00\x22" + strings.Repeat("x", 4096)
	head, body, err := ReadHeader(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if len(head) != HeaderSize {
		t.Errorf("len(head) = %d, want %d", len(head), HeaderSize)
	}

	got, err := Sniff(head)
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if got.MIME != "audio/flac" {
		t.Errorf("MIME = %q, want audio/flac", got.MIME)
	}

	all, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all) != len(payload) {
		t.Errorf("replayed %d bytes, want %d", len(all), len(payload))
	}
}

func TestSniff_ImagesAndArchives(t *testing.T) {
	for name, tc := range map[string]struct {
		head string
		mime string
	}{
		"png":  {"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png"},
		"jpeg": {"\xff\xd8\xff\xe0\x00\x10JFIF", "image/jpeg"},
		"gif":  {"GIF89a\x01\x00", "image/gif"},
		"webp": {"RIFF\x24\x00\x00\x00WEBPVP8 ", "image/webp"},
		"docx": {"PK\x03\x04\x14\x00\x06\x00", "application/zip"},
	} {
		got, err := Sniff([]byte(tc.head))
		if err != nil || got.MIME != tc.mime {
			t.Errorf("%s: got %+v, %v", name, got, err)
		}
	}
}

func TestSniffMatching_OfficeDocumentsAreZip(t *testing.T) {
	docx := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if _, err := SniffMatching([]byte("PK\x03\x04rest"), docx); err != nil {
		t.Fatalf("a docx is a zip: %v", err)
	}
	if _, err := SniffMatching([]byte("MZ\x90\x00"), docx); err == nil {
		t.Fatal("an executable passed for a docx")
	}
	if _, err := SniffMatching([]byte("\xff\xd8\xff\xe0"), "image/png"); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("a jpeg declared as png: %v", err)
	}
}

func TestSniffable(t *testing.T) {
	for declared, want := range map[string]bool{
		"application/pdf":          true,
		"image/jpeg":               true,
		"video/mp4":                true,
		"application/zip":          true,
		"text/plain":               false,
		"text/csv":                 false,
		"application/octet-stream": false,
		"":                         false,
	} {
		if got := Sniffable(declared); got != want {
			t.Errorf("Sniffable(%q) = %v", declared, got)
		}
	}
}

func TestExecutable(t *testing.T) {
	for head, want := range map[string]string{
		"MZ\x90\x00\x03":       "windows",
		"\x7fELF\x02\x01":      "elf",
		"\xcf\xfa\xed\xfe\x07": "macho",
		"\xca\xfe\xba\xbe\x00": "macho",
		"#!/bin/sh\necho":      "script",
	} {
		if kind, ok := Executable([]byte(head)); !ok || kind != want {
			t.Errorf("Executable(%q) = %q, %v", head, kind, ok)
		}
	}
	for _, head := range []string{"%PDF-1.7", "PK\x03\x04", "hello", "", "M"} {
		if kind, ok := Executable([]byte(head)); ok {
			t.Errorf("Executable(%q) = %q", head, kind)
		}
	}
}
