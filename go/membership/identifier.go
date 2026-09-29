package membership

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrReserved          = errors.New("identifier reserved")
	ErrInvalidIdentifier = errors.New("identifier invalid")
)

// DefaultPattern is the local part Messag-style products hand out: lower-case
// letters and digits, with single dots, dashes or underscores between them.
var DefaultPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// StandardReserved are the names no sign-up may obtain under any owned
// domain: the RFC 2142 mailboxes, the CA/Browser Forum domain-validation
// names, report sinks and the daemons. Keys carry no separator.
var StandardReserved = []string{
	"abuse", "admin", "administrator", "hostmaster", "postmaster", "webmaster",
	"info", "marketing", "sales", "support", "security", "noc", "root",
	"mailerdaemon", "noreply", "donotreply", "mailer", "daemon", "system",
	"dmarc", "tlsrpt", "bounce", "bounces", "www", "mail", "mx", "smtp",
	"imap", "jmap", "ftp", "ssladmin", "sysadmin",
}

// IdentifierPolicy is the rule for the local part of an address under a
// domain the product owns. The zero value is a product without one.
type IdentifierPolicy struct {
	OwnedDomain string
	// Reserved holds names refused on top of StandardReserved, without
	// separators: "post.master" and "post_master" both match "postmaster".
	Reserved []string
	// Pattern defaults to DefaultPattern.
	Pattern *regexp.Regexp
}

var separators = strings.NewReplacer(".", "", "_", "", "-", "")

// Check answers ErrInvalidIdentifier or ErrReserved for a local part, nil when
// a sign-up may obtain it.
func (p IdentifierPolicy) Check(local string) error {
	pattern := p.Pattern
	if pattern == nil {
		pattern = DefaultPattern
	}
	if !pattern.MatchString(local) {
		return ErrInvalidIdentifier
	}
	bare := separators.Replace(local)
	for _, r := range StandardReserved {
		if bare == r {
			return ErrReserved
		}
	}
	for _, r := range p.Reserved {
		if bare == separators.Replace(strings.ToLower(r)) {
			return ErrReserved
		}
	}
	return nil
}

// OwnedLocalPart is the lower-cased local part of email when it is under the
// owned domain, and whether it is.
func (p IdentifierPolicy) OwnedLocalPart(email string) (string, bool) {
	if p.OwnedDomain == "" {
		return "", false
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || !strings.EqualFold(email[at+1:], p.OwnedDomain) {
		return "", false
	}
	return strings.ToLower(email[:at]), true
}

func (p IdentifierPolicy) String() string {
	if p.OwnedDomain == "" {
		return "no owned domain"
	}
	return fmt.Sprintf("owned domain %s, %d reserved names", p.OwnedDomain, len(p.Reserved)+len(StandardReserved))
}
