package botguard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	errTokenMalformed = errors.New("malformed token")
	errTokenSignature = errors.New("bad token signature")
	errTokenAudience  = errors.New("token for another deployment")
	errTokenExpired   = errors.New("token expired")
	errTokenNetwork   = errors.New("token from another network")
)

type tokenClaims struct {
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	IP  string `json:"ip"`
}

type keys struct {
	challenge, keySignature, token, network, form []byte
}

// deriveKeys splits the one deployment secret into independent keys, so a
// value signed for one purpose is never accepted for another.
func deriveKeys(secret []byte) keys {
	sub := func(label string) []byte {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte("botguard/" + label))
		return m.Sum(nil)
	}
	return keys{
		challenge:    sub("challenge"),
		keySignature: sub("key-signature"),
		token:        sub("token"),
		network:      sub("network"),
		form:         sub("form"),
	}
}

func sign(key []byte, data string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (k keys) networkHash(net string) string { return sign(k.network, net)[:22] }

func (k keys) issueToken(aud, net string, ttl time.Duration, now time.Time) string {
	body, _ := json.Marshal(tokenClaims{Aud: aud, Exp: now.Add(ttl).Unix(), IP: k.networkHash(net)})
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + sign(k.token, payload)
}

func (k keys) checkToken(token, aud, net string, now time.Time) error {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" {
		return errTokenMalformed
	}
	if !hmac.Equal([]byte(sig), []byte(sign(k.token, payload))) {
		return errTokenSignature
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return errTokenMalformed
	}
	var c tokenClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return errTokenMalformed
	}
	switch {
	case c.Aud != aud:
		return errTokenAudience
	case now.Unix() >= c.Exp:
		return errTokenExpired
	case !hmac.Equal([]byte(c.IP), []byte(k.networkHash(net))):
		return errTokenNetwork
	}
	return nil
}
