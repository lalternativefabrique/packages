package connect

import "time"

type ProductConnectionDTO struct {
	Product     string     `json:"product" validate:"required"`
	Label       string     `json:"label" validate:"required"`
	Status      string     `json:"status" validate:"required" enums:"none,active,revoked"`
	Scopes      []string   `json:"scopes" validate:"required"`
	ConnectedAt *time.Time `json:"connected_at" validate:"required" extensions:"x-nullable"`
}

type ConnectionsDTO struct {
	Available bool                   `json:"available" validate:"required"`
	Products  []ProductConnectionDTO `json:"products" validate:"required"`
}

type StartDTO struct {
	AuthorizeURL string `json:"authorize_url" validate:"required"`
}

type CallbackDTO struct {
	Product string `json:"product" validate:"required"`
	Outcome string `json:"outcome" validate:"required" enums:"connected,refused,invalid,unavailable"`
}

type ScopeDTO struct {
	Name  string `json:"name" validate:"required"`
	Label string `json:"label" validate:"required"`
}

// LinkDTO is what the host made of a resource; App is the linked entity's id.
type LinkDTO struct {
	State string `json:"state" validate:"required" enums:"none,linked,ignored"`
	App   string `json:"app" validate:"required"`
}

// ResourceDTO is one shared resource, in tree order: a child follows its
// parent, one level deeper.
type ResourceDTO struct {
	ID        string  `json:"id" validate:"required"`
	Type      string  `json:"type" validate:"required"`
	Label     string  `json:"label" validate:"required"`
	URL       string  `json:"url" validate:"required"`
	Parent    string  `json:"parent" validate:"required"`
	Depth     int     `json:"depth" validate:"required"`
	Mappable  bool    `json:"mappable" validate:"required"`
	Suggested string  `json:"suggested" validate:"required"`
	Link      LinkDTO `json:"link" validate:"required"`
}

type ResourcesDTO struct {
	Product   string        `json:"product" validate:"required"`
	Scopes    []ScopeDTO    `json:"scopes" validate:"required"`
	Resources []ResourceDTO `json:"resources" validate:"required"`
}

type SetLinkRequest struct {
	ResourceID string `json:"resource_id" validate:"required"`
	Choice     string `json:"choice" validate:"required" enums:"link,create,ignore,clear"`
	App        string `json:"app"`
}

func NewLinkDTO(l Link) LinkDTO {
	switch {
	case l.Ignored:
		return LinkDTO{State: "ignored"}
	case l.Entity != "":
		return LinkDTO{State: "linked", App: l.Entity}
	default:
		return LinkDTO{State: "none"}
	}
}

func NewResourcesDTO(product string, g Grant, links map[string]Link, mappable func(Resource) bool, entities []Entity) ResourcesDTO {
	out := ResourcesDTO{Product: product, Scopes: make([]ScopeDTO, 0, len(g.Scopes)), Resources: []ResourceDTO{}}
	for _, sc := range g.Scopes {
		out.Scopes = append(out.Scopes, ScopeDTO{Name: sc.Name, Label: sc.Label})
	}
	for _, p := range Tree(g.Resources) {
		canLink := mappable == nil || mappable(p.Resource)
		suggested := ""
		if canLink {
			suggested = Suggest(p.Resource, entities)
		}
		out.Resources = append(out.Resources, ResourceDTO{
			ID: p.ID, Type: p.Type, Label: p.Label, URL: p.URL, Parent: p.Parent, Depth: p.Depth,
			Mappable: canLink, Suggested: suggested, Link: NewLinkDTO(links[p.ID]),
		})
	}
	return out
}
