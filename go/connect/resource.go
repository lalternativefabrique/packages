package connect

import (
	"net/url"
	"strings"
)

type Resource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Label  string `json:"label"`
	Parent string `json:"parent,omitempty"`
	URL    string `json:"url,omitempty"`
}

type Scope struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Grant is what a product answers on /connect/v1/resources: what the grant
// reaches that the person still holds.
type Grant struct {
	Scopes    []Scope    `json:"scopes"`
	Resources []Resource `json:"resources"`
}

func (g Grant) IDs() []string {
	ids := make([]string, 0, len(g.Resources))
	for _, r := range g.Resources {
		ids = append(ids, r.ID)
	}
	return ids
}

func (g Grant) Resource(id string) (Resource, bool) {
	for _, r := range g.Resources {
		if r.ID == id {
			return r, true
		}
	}
	return Resource{}, false
}

// Lost is what a grant reached before and no longer does.
func Lost(before, now []string) []string {
	still := make(map[string]bool, len(now))
	for _, id := range now {
		still[id] = true
	}
	var lost []string
	for _, id := range before {
		if !still[id] {
			lost = append(lost, id)
		}
	}
	return lost
}

type Placed struct {
	Resource
	Depth int
}

// Tree orders resources parent first, each child under its parent, keeping the
// product's order among siblings. A resource whose parent was not shared
// stands at the root.
func Tree(resources []Resource) []Placed {
	known := make(map[string]bool, len(resources))
	for _, r := range resources {
		known[r.ID] = true
	}
	children := map[string][]Resource{}
	var roots []Resource
	for _, r := range resources {
		if r.Parent != "" && r.Parent != r.ID && known[r.Parent] {
			children[r.Parent] = append(children[r.Parent], r)
		} else {
			roots = append(roots, r)
		}
	}
	out := make([]Placed, 0, len(resources))
	placed := map[string]bool{}
	var walk func(r Resource, depth int)
	walk = func(r Resource, depth int) {
		if placed[r.ID] {
			return
		}
		placed[r.ID] = true
		out = append(out, Placed{Resource: r, Depth: depth})
		for _, c := range children[r.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	for _, r := range resources {
		walk(r, 0)
	}
	return out
}

// NormalizeDomain is the bare host of a site or URL, without "www.".
func NormalizeDomain(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// Host is the domain a resource's site is matched on, "" without a site.
func Host(r Resource) string {
	if r.URL == "" {
		return ""
	}
	return NormalizeDomain(r.URL)
}

// Entity is what the host links a resource to: one of its own records.
type Entity struct {
	ID      string
	Name    string
	Domains []string
}

// Suggest names the entity whose domains hold the resource's site.
func Suggest(r Resource, entities []Entity) string {
	host := Host(r)
	if host == "" {
		return ""
	}
	for _, e := range entities {
		for _, d := range e.Domains {
			if d == host {
				return e.ID
			}
		}
	}
	return ""
}

type Choice string

const (
	ChoiceLink   Choice = "link"
	ChoiceCreate Choice = "create"
	ChoiceIgnore Choice = "ignore"
	ChoiceClear  Choice = "clear"
)

// Link is what the host made of a shared resource. Entity "" with Ignored
// false means nothing was decided yet.
type Link struct {
	ResourceID string
	Entity     string
	Ignored    bool
}

// Choosable is the shared resource a choice may be made on: one the product
// still lists for this grant, and one mappable says can be linked.
func Choosable(g Grant, resourceID string, mappable func(Resource) bool) (Resource, error) {
	r, ok := g.Resource(resourceID)
	if !ok {
		return r, ErrUnknownResource
	}
	if mappable != nil && !mappable(r) {
		return r, ErrNotMappable
	}
	return r, nil
}
