// Package fileguard holds the checks every file-ingestion path needs, in
// composable stages rather than one fixed chain: callers that handle media run
// all of them, callers that only store bytes run the first two.
package fileguard

import (
	"errors"
	"fmt"
	"strings"
)

// HeaderSize is how many leading bytes Sniff needs. Every signature it knows
// lives in the first 12, but callers reading from a network stream should hand
// over a larger window so one read serves both sniffing and the copy that
// follows.
const HeaderSize = 512

// ErrUnknownContainer reports bytes matching no known container. It is a
// rejection, not a fallback: a caller that cannot name the container cannot
// claim to have validated it.
var ErrUnknownContainer = errors.New("fileguard: unrecognised container")

// ErrTypeMismatch reports bytes whose real container contradicts the type the
// caller declared.
var ErrTypeMismatch = errors.New("fileguard: declared type does not match content")

// Container is a media container identified from magic bytes.
type Container struct {
	// MIME is the canonical type for the container, chosen to match what upload
	// allowlists declare so a sniffed value can be compared against them.
	MIME string
	// Ext is the filename extension, with the dot. Some transcription providers
	// derive the format from the filename and fail on a bare name, so callers
	// naming a temp file need this.
	Ext string
}

// ErrNotMedia reports a recognised container that is neither audio nor video,
// on a path that only accepts those.
var ErrNotMedia = errors.New("fileguard: not an audio or video container")

// IsMedia reports whether the container carries audio or video. Sniff also
// recognises documents, so a media path must ask rather than assume.
func (c Container) IsMedia() bool {
	return strings.HasPrefix(c.MIME, "audio/") || strings.HasPrefix(c.MIME, "video/")
}

// Sniff identifies the container from the leading bytes of a file.
//
// It deliberately returns an error rather than guessing. An earlier sniffer in
// the upload worker fell through to .m4a for unrecognised bytes so that an
// unusual container still got a plausible filename; that default is exactly
// what stops a sniffer from being a gate, since nothing can fail. Callers that
// want the old lenient behaviour should handle ErrUnknownContainer explicitly
// and choose their own fallback, which keeps the choice visible.
func Sniff(head []byte) (Container, error) {
	switch {
	case len(head) >= 3 && string(head[:3]) == "ID3":
		return Container{MIME: "audio/mpeg", Ext: ".mp3"}, nil
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xF6 == 0xF0:
		// ADTS sync (12 bits set, layer bits 00): raw AAC. Checked before MP3,
		// whose 11-bit sync would otherwise claim it.
		return Container{MIME: "audio/aac", Ext: ".aac"}, nil
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0 && head[1]&0x06 != 0:
		// MPEG audio frame sync with a valid layer: an MP3 carrying no ID3 tag.
		return Container{MIME: "audio/mpeg", Ext: ".mp3"}, nil
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return Container{MIME: "audio/wav", Ext: ".wav"}, nil
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		// MP4, M4A and most video containers share the ftyp box, so the bytes
		// alone cannot tell audio from video here. Callers needing that
		// distinction get it from Probe, which reads the actual streams.
		return Container{MIME: "video/mp4", Ext: ".mp4"}, nil
	case len(head) >= 4 && string(head[:4]) == "OggS":
		return Container{MIME: "audio/ogg", Ext: ".ogg"}, nil
	case len(head) >= 4 && string(head[:4]) == "fLaC":
		return Container{MIME: "audio/flac", Ext: ".flac"}, nil
	case len(head) >= 4 && string(head[:4]) == "\x1aE\xdf\xa3":
		// EBML header: WebM or Matroska.
		return Container{MIME: "video/webm", Ext: ".webm"}, nil
	case len(head) >= 5 && string(head[:5]) == "%PDF-":
		return Container{MIME: "application/pdf", Ext: ".pdf"}, nil
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return Container{MIME: "image/png", Ext: ".png"}, nil
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return Container{MIME: "image/jpeg", Ext: ".jpg"}, nil
	case len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a"):
		return Container{MIME: "image/gif", Ext: ".gif"}, nil
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return Container{MIME: "image/webp", Ext: ".webp"}, nil
	case len(head) >= 4 && string(head[:4]) == "PK\x03\x04":
		// Every zip-based format — docx, xlsx, odt, epub, jar — opens this way;
		// telling them apart means reading the archive, not its first bytes.
		return Container{MIME: "application/zip", Ext: ".zip"}, nil
	}
	return Container{}, ErrUnknownContainer
}

// SniffMatching identifies the container and checks it against a declared type,
// which is what an upload path wants: the allowlist decides what may be sent,
// and this decides whether the bytes kept that promise.
//
// Matching is by family and container, not by exact string. A declared
// "audio/x-m4a" and a sniffed "video/mp4" describe the same ftyp box, and a
// caller that demuxes audio out of a video container accepts both; requiring
// string equality would reject correct uploads while catching nothing real.
func SniffMatching(head []byte, declared string) (Container, error) {
	got, err := Sniff(head)
	if err != nil {
		return Container{}, err
	}
	if !compatible(got.MIME, declared) {
		return Container{}, fmt.Errorf("%w: declared %q, content is %s", ErrTypeMismatch, declared, got.MIME)
	}
	return got, nil
}

// compatible reports whether sniffed content honours a declared type. It maps
// both sides onto the container the bytes actually carry, since one container
// is spelled several ways across clients and specs.
func compatible(sniffed, declared string) bool {
	return containerOf(sniffed) == containerOf(declared)
}

func containerOf(mime string) string {
	base, _, _ := strings.Cut(mime, ";")
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "audio/mpeg", "audio/mp3":
		return "mpeg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/mp4", "audio/x-m4a", "audio/mp4a-latm", "audio/aac",
		"video/mp4", "video/quicktime", "video/mpeg":
		// All ftyp-boxed. Sniff cannot separate them and neither should this.
		return "mp4"
	case "audio/ogg", "audio/opus", "application/ogg":
		return "ogg"
	case "audio/flac", "audio/x-flac":
		return "flac"
	case "audio/webm", "video/webm", "video/x-matroska":
		return "ebml"
	case "application/pdf":
		return "pdf"
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "application/zip", "application/x-zip-compressed", "application/epub+zip", "application/java-archive",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.oasis.opendocument.text",
		"application/vnd.oasis.opendocument.spreadsheet",
		"application/vnd.oasis.opendocument.presentation":
		return "zip"
	}
	return "unknown:" + base
}

// Sniffable reports whether a declared type names a container Sniff can
// recognise, which is when SniffMatching can hold the bytes to the claim. A
// store that accepts any file checks only those and lets the rest through.
func Sniffable(declared string) bool {
	return !strings.HasPrefix(containerOf(declared), "unknown:")
}

// Executable reports a program by its leading bytes: a Windows PE, an ELF
// binary, a Mach-O binary or a script with an interpreter line. It names
// the kind, so a caller can say what was found. A program is not malware;
// this exists for the case where it hides behind another file's name.
func Executable(head []byte) (string, bool) {
	switch {
	case len(head) >= 2 && head[0] == 'M' && head[1] == 'Z':
		return "windows", true
	case len(head) >= 4 && string(head[:4]) == "\x7fELF":
		return "elf", true
	case len(head) >= 4 && isMachO(head[:4]):
		return "macho", true
	case len(head) >= 2 && head[0] == '#' && head[1] == '!':
		return "script", true
	}
	return "", false
}

func isMachO(magic []byte) bool {
	switch string(magic) {
	case "\xfe\xed\xfa\xce", "\xfe\xed\xfa\xcf", "\xce\xfa\xed\xfe", "\xcf\xfa\xed\xfe", "\xca\xfe\xba\xbe":
		return true
	}
	return false
}
