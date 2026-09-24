package host

import (
	"crypto/ed25519"
	"errors"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/golang-jwt/jwt/v5"
)

// provenSubject reads the person a turn acts for from its subject token,
// signed by the caller for this agent.
func provenSubject(msg *a2a.Message, key ed25519.PublicKey, agentName string) (string, error) {
	var raw string
	if msg != nil {
		raw, _ = msg.Metadata[SubjectTokenKey].(string)
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("no subject token")
	}
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(agentName),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return "", errors.New("the token names no subject")
	}
	return claims.Subject, nil
}

// conversationKey keeps each person's history of a context apart: a
// context id is the caller's to choose, and two people may pick the same.
func conversationKey(subject, contextID string) string {
	return subject + "\x00" + contextID
}
