package membership

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Report is what Reconcile found. It corrects nothing: every line is for an
// operator to look at.
type Report struct {
	// IdentitiesWithoutMember carry the role at urbangate and were never met
	// here: a person who signed up and never came back, or a lost sign-up.
	IdentitiesWithoutMember []string
	// MembersWithoutIdentity are ready members whose role is gone at
	// urbangate: a deletion that never reached this core.
	MembersWithoutIdentity []string
	// ResourcesWithoutMember exist in the external system under a key no
	// member holds: created out of band, or left by an old flow.
	ResourcesWithoutMember []string
	// MembersWithoutResource are ready members whose resource is gone.
	MembersWithoutResource []string
	Conflicts              []string
}

func (r Report) Clean() bool {
	return len(r.IdentitiesWithoutMember) == 0 && len(r.MembersWithoutIdentity) == 0 &&
		len(r.ResourcesWithoutMember) == 0 && len(r.MembersWithoutResource) == 0 && len(r.Conflicts) == 0
}

// Reconcile compares urbangate's identities carrying the product's role, the
// members, and the resources the external system holds. Product.Identities
// is required.
func (s *Service) Reconcile(ctx context.Context) (Report, error) {
	if s.p.Identities == nil {
		return Report{}, errors.New("membership: Product.Identities is required to reconcile")
	}
	identities, err := s.p.Identities.WithRole(ctx, s.p.Role())
	if err != nil {
		return Report{}, fmt.Errorf("identities: %w", err)
	}
	members, err := s.store.List(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("members: %w", err)
	}
	resources, err := s.p.Resources.List(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("resources: %w", err)
	}

	var r Report
	byIdentity := map[string]Member{}
	byKey := map[string]Member{}
	for _, m := range members {
		byIdentity[m.IdentityID] = m
		if m.State != StateErased {
			byKey[m.Key()] = m
		}
		if m.State == StateConflict {
			r.Conflicts = append(r.Conflicts, m.IdentityID+": "+m.LastError)
		}
	}
	known := map[string]bool{}
	for _, id := range identities {
		known[id] = true
		if _, ok := byIdentity[id]; !ok {
			r.IdentitiesWithoutMember = append(r.IdentitiesWithoutMember, id)
		}
	}
	held := map[string]bool{}
	for _, key := range resources {
		held[key] = true
		if _, ok := byKey[key]; !ok {
			r.ResourcesWithoutMember = append(r.ResourcesWithoutMember, key)
		}
	}
	for _, m := range members {
		if m.State != StateReady {
			continue
		}
		if !known[m.IdentityID] {
			r.MembersWithoutIdentity = append(r.MembersWithoutIdentity, m.IdentityID)
		}
		if !held[m.Key()] {
			r.MembersWithoutResource = append(r.MembersWithoutResource, m.IdentityID)
		}
	}
	for _, list := range [][]string{r.IdentitiesWithoutMember, r.MembersWithoutIdentity, r.ResourcesWithoutMember, r.MembersWithoutResource, r.Conflicts} {
		sort.Strings(list)
	}
	return r, nil
}
