// Package authz asks one question everywhere in the suite: may this subject
// perform this action on this resource, in this context? The question and
// its answer follow the OpenID AuthZEN Evaluation API, so a core, an agent or
// a tool never embeds a rule of its own: it builds the request and asks a
// PDP, embedded or remote.
package authz

import (
	"context"

	"github.com/lalternative/packages/go/svcauth"
)

// Subject is who asks, as the suite's identity layer names it: the person
// behind a personal key (Owner), the end user an app acts for (EndUser), the
// OAuth2 client (ClientID), and the coarse roles and scopes of the token.
type Subject struct {
	Owner    string
	EndUser  string
	ClientID string
	Roles    []string
	Scopes   []string
}

// SubjectFromClaims builds the subject of a verified token.
func SubjectFromClaims(c svcauth.Claims) Subject {
	return Subject{
		Owner:    c.Owner,
		EndUser:  "",
		ClientID: c.ClientID,
		Roles:    c.Roles,
		Scopes:   c.Scopes,
	}
}

// Type is "user" when a person stands behind the subject, "machine" otherwise.
func (s Subject) Type() string {
	if s.Owner != "" {
		return "user"
	}
	return "machine"
}

// ID is the owner's identity id, or the client id for a pure machine.
func (s Subject) ID() string {
	if s.Owner != "" {
		return s.Owner
	}
	return s.ClientID
}

type Action struct {
	Name       string
	Properties map[string]any
}

// Resource names what the action targets: a product's type and the id
// within it, so the wire form reads <product>:<type>:<id>.
type Resource struct {
	Type       string
	ID         string
	Properties map[string]any
}

type Request struct {
	Subject  Subject
	Action   Action
	Resource Resource
	Context  map[string]any
}

const (
	ReasonApprovalRequired = "approval_required"
	ReasonDeniedByPolicy   = "denied_by_policy"
	ReasonNoGrant          = "no_grant"
)

// Decision is the answer. Reason is the wire context's "reason" when the
// policy gave one; Context carries the rest of it.
type Decision struct {
	Allow   bool
	Reason  string
	Context map[string]any
}

// PDP is a policy decision point: an embedded engine, a remote one, or a
// stub.
type PDP interface {
	Evaluate(ctx context.Context, req Request) (Decision, error)
}

// PDPFunc adapts a function to the PDP interface.
type PDPFunc func(ctx context.Context, req Request) (Decision, error)

func (f PDPFunc) Evaluate(ctx context.Context, req Request) (Decision, error) {
	return f(ctx, req)
}

// AllowAll answers yes to everything: the desktop sidecar, where the person
// asking owns the machine, and tests.
func AllowAll() PDP {
	return PDPFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Allow: true}, nil
	})
}

// DenyAll answers no to everything, with ReasonDeniedByPolicy.
func DenyAll() PDP {
	return PDPFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Allow: false, Reason: ReasonDeniedByPolicy}, nil
	})
}
