package audioreader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// CacheKey names where a reading of text is kept.
//
// The text is hashed into the key so that an edit or a language switch misses
// and is read again, rather than serving audio of words the reader can no
// longer see.
//
// scope separates readings that share an id: a note and an article can belong
// to the same entity, and a single key would have one served as the other.
func CacheKey(scope, id, text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%s/%s-%s.mp3", scope, id, hex.EncodeToString(sum[:])[:16])
}

// OpeningKey names the opening of a reading, kept apart from the whole.
//
// Apart, because the two are not interchangeable: a listener handed the
// opening under the whole reading's key would hear the first paragraph and
// then silence, with nothing to say the rest exists.
func OpeningKey(scope, id, text string) string {
	return CacheKey(scope, id, text) + ".opening"
}
