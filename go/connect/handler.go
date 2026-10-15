package connect

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// Who is the signed-in person as the host scopes them: Owner keys the grant,
// Scope keys the links (an organization, or the owner again).
type Who struct {
	Owner string
	Scope string
}

// Linker is the host's side of linking a shared resource to one of its own
// entities. Links belong to the scope and outlive the grant that reached them.
type Linker interface {
	Entities(ctx context.Context, scope string) ([]Entity, error)
	// Entity is ErrUnknownEntity when the scope holds no such entity.
	Entity(ctx context.Context, scope, id string) (Entity, error)
	// Create makes an entity from the resource; ErrEntityExists when its name
	// is taken.
	Create(ctx context.Context, scope string, r Resource) (Entity, error)
	Links(ctx context.Context, scope, product string) (map[string]Link, error)
	LinkedElsewhere(ctx context.Context, scope, resourceID string) (bool, error)
	// SetLink is ErrLinkedElsewhere when another scope took the resource.
	SetLink(ctx context.Context, scope, product string, l Link) error
	ClearLink(ctx context.Context, scope, product, resourceID string) error
}

type HandlerConfig struct {
	Identify func(c echo.Context) (Who, error)
	// Linker is optional: without it resources are listed with no link and
	// links cannot be set.
	Linker Linker
	// Mappable says whether a resource can be linked; nil links any.
	Mappable func(Resource) bool
	// CallbackRedirect, when set, makes the callback redirect there with
	// ?<product>=connected|error&reason=<outcome> instead of answering JSON.
	CallbackRedirect string
	// CallbackWithoutSession finishes the consent from its state alone, for a
	// callback served where the browser carries no session.
	CallbackWithoutSession bool
}

type Handler struct {
	grants *Grants
	cfg    HandlerConfig
}

func NewHandler(g *Grants, cfg HandlerConfig) *Handler {
	return &Handler{grants: g, cfg: cfg}
}

// Register mounts the routes on a group the host already authenticated, the
// callback included unless CallbackWithoutSession asks to mount it apart with
// RegisterCallback. The middlewares guard every route but the callback.
func (h *Handler) Register(g *echo.Group, mw ...echo.MiddlewareFunc) {
	g.GET("/connections", h.List, mw...)
	if !h.cfg.CallbackWithoutSession {
		h.RegisterCallback(g)
	}
	g.POST("/connections/:product", h.Start, mw...)
	g.DELETE("/connections/:product", h.Disconnect, mw...)
	g.GET("/connections/:product/resources", h.Resources, mw...)
	g.PUT("/connections/:product/links", h.SetLink, mw...)
}

func (h *Handler) RegisterCallback(g *echo.Group) {
	g.GET("/connections/callback", h.Callback)
}

func failure(c echo.Context, status int, reason string) error {
	return c.JSON(status, map[string]string{"error": reason})
}

func (h *Handler) productFailure(c echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrUnknownProduct):
		return failure(c, http.StatusNotFound, "product")
	case errors.Is(err, ErrNotConnected):
		return failure(c, http.StatusNotFound, "not_connected")
	case errors.Is(err, ErrAccessRemoved):
		return failure(c, http.StatusConflict, "access_removed")
	case errors.Is(err, ErrRevokeFailed):
		log.Printf("connect: %v", err)
		return failure(c, http.StatusServiceUnavailable, "urbangate_unavailable")
	case errors.Is(err, ErrUnavailable) && !h.grants.Available():
		return failure(c, http.StatusServiceUnavailable, "connect_unavailable")
	case errors.Is(err, ErrUnavailable):
		log.Printf("connect: %v", err)
		return failure(c, http.StatusServiceUnavailable, "product_unavailable")
	}
	return err
}

// List godoc
// @Summary  The products the signed-in person may connect, and where each connection stands
// @Tags     connect
// @Produce  json
// @Success  200  {object}  ConnectionsDTO
// @Router   /connections [get]
// @ID       listConnections
func (h *Handler) List(c echo.Context) error {
	who, err := h.cfg.Identify(c)
	if err != nil {
		return err
	}
	byProduct, err := h.grants.Connections(c.Request().Context(), who.Owner)
	if err != nil {
		return err
	}
	offered := h.grants.Products().Offered()
	out := ConnectionsDTO{Available: h.grants.Available(), Products: make([]ProductConnectionDTO, 0, len(offered))}
	for _, p := range offered {
		dto := ProductConnectionDTO{Product: p.ID, Label: p.Label, Status: "none", Scopes: []string{}}
		if conn, ok := byProduct[p.ID]; ok {
			at := conn.ConnectedAt
			dto.Status, dto.Scopes, dto.ConnectedAt = string(conn.Status), conn.Scopes, &at
			if dto.Scopes == nil {
				dto.Scopes = []string{}
			}
		}
		out.Products = append(out.Products, dto)
	}
	return c.JSON(http.StatusOK, out)
}

// Start godoc
// @Summary  Open urbangate's consent screen for one product; the person chooses there what is shared
// @Tags     connect
// @Produce  json
// @Param    product  path      string  true  "product id"
// @Success  200      {object}  StartDTO
// @Failure  404      {object}  map[string]string
// @Failure  503      {object}  map[string]string
// @Router   /connections/{product} [post]
// @ID       startConnection
func (h *Handler) Start(c echo.Context) error {
	who, err := h.cfg.Identify(c)
	if err != nil {
		return err
	}
	to, err := h.grants.Start(c.Request().Context(), who.Owner, c.Param("product"))
	if err != nil {
		return h.productFailure(c, err)
	}
	return c.JSON(http.StatusOK, StartDTO{AuthorizeURL: to})
}

// Callback godoc
// @Summary  Finish a connection with what urbangate sent back to the redirect URI
// @Tags     connect
// @Produce  json
// @Param    state  query     string  true   "state"
// @Param    code   query     string  false  "authorization code"
// @Param    error  query     string  false  "access_denied when the person refused"
// @Success  200    {object}  CallbackDTO
// @Router   /connections/callback [get]
// @ID       finishConnection
func (h *Handler) Callback(c echo.Context) error {
	state, code, authErr := c.QueryParam("state"), c.QueryParam("code"), c.QueryParam("error")
	var (
		product string
		outcome Outcome
		err     error
	)
	if h.cfg.CallbackWithoutSession {
		product, outcome, err = h.grants.FinishByState(c.Request().Context(), state, code, authErr)
	} else {
		who, ierr := h.cfg.Identify(c)
		if ierr != nil {
			return ierr
		}
		product, outcome, err = h.grants.Finish(c.Request().Context(), who.Owner, state, code, authErr)
	}
	if err != nil {
		return err
	}
	if h.cfg.CallbackRedirect == "" {
		return c.JSON(http.StatusOK, CallbackDTO{Product: product, Outcome: string(outcome)})
	}
	return c.Redirect(http.StatusFound, callbackTarget(h.cfg.CallbackRedirect, product, outcome))
}

func callbackTarget(base, product string, outcome Outcome) string {
	result := "error"
	if outcome == OutcomeConnected {
		result = "connected"
	}
	if product == "" {
		product = "connect"
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	target := base + sep + url.QueryEscape(product) + "=" + result
	if outcome != OutcomeConnected && outcome != "" {
		target += "&reason=" + url.QueryEscape(string(outcome))
	}
	return target
}

// Resources godoc
// @Summary  What the person shared from the product, in tree order, with what was made of each
// @Tags     connect
// @Produce  json
// @Param    product  path      string  true  "product id"
// @Success  200      {object}  ResourcesDTO
// @Failure  404      {object}  map[string]string
// @Failure  409      {object}  map[string]string  "access_removed: the person removed the access on urbangate"
// @Failure  503      {object}  map[string]string
// @Router   /connections/{product}/resources [get]
// @ID       listConnectionResources
func (h *Handler) Resources(c echo.Context) error {
	who, err := h.cfg.Identify(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	product := c.Param("product")
	grant, err := h.grants.Resources(ctx, who.Owner, product)
	if err != nil {
		return h.productFailure(c, err)
	}
	links := map[string]Link{}
	var entities []Entity
	if h.cfg.Linker != nil {
		if links, err = h.cfg.Linker.Links(ctx, who.Scope, product); err != nil {
			return err
		}
		if entities, err = h.cfg.Linker.Entities(ctx, who.Scope); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, NewResourcesDTO(product, grant, links, h.cfg.Mappable, entities))
}

// SetLink godoc
// @Summary  Link a shared resource to an entity, create the entity from it, ignore it, or undo that choice
// @Tags     connect
// @Accept   json
// @Produce  json
// @Param    product  path      string          true  "product id"
// @Param    body     body      SetLinkRequest  true  "choice"
// @Success  200      {object}  LinkDTO
// @Failure  404      {object}  map[string]string
// @Failure  409      {object}  map[string]string
// @Failure  422      {object}  map[string]string
// @Failure  503      {object}  map[string]string
// @Router   /connections/{product}/links [put]
// @ID       setConnectionLink
func (h *Handler) SetLink(c echo.Context) error {
	who, err := h.cfg.Identify(c)
	if err != nil {
		return err
	}
	product := c.Param("product")
	if _, err := h.grants.Product(product); err != nil {
		return h.productFailure(c, err)
	}
	if h.cfg.Linker == nil {
		return failure(c, http.StatusNotFound, "links")
	}
	var in SetLinkRequest
	if err := c.Bind(&in); err != nil || in.ResourceID == "" {
		return failure(c, http.StatusUnprocessableEntity, "resource_id")
	}
	ctx := c.Request().Context()
	grant, err := h.grants.Resources(ctx, who.Owner, product)
	if err != nil {
		return h.productFailure(c, err)
	}
	r, err := Choosable(grant, in.ResourceID, h.cfg.Mappable)
	if errors.Is(err, ErrNotMappable) {
		return failure(c, http.StatusUnprocessableEntity, "not_an_app")
	}
	if err != nil {
		return failure(c, http.StatusUnprocessableEntity, "resource")
	}
	linker := h.cfg.Linker
	choice := Choice(in.Choice)
	if choice == ChoiceLink || choice == ChoiceCreate {
		elsewhere, err := linker.LinkedElsewhere(ctx, who.Scope, r.ID)
		if err != nil {
			return err
		}
		if elsewhere {
			return failure(c, http.StatusConflict, "linked_elsewhere")
		}
	}
	var link Link
	switch choice {
	case ChoiceClear:
		if err := linker.ClearLink(ctx, who.Scope, product, r.ID); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, NewLinkDTO(Link{}))
	case ChoiceIgnore:
		link = Link{ResourceID: r.ID, Ignored: true}
	case ChoiceLink:
		e, err := linker.Entity(ctx, who.Scope, strings.TrimSpace(in.App))
		if errors.Is(err, ErrUnknownEntity) {
			return failure(c, http.StatusNotFound, "app")
		}
		if err != nil {
			return err
		}
		link = Link{ResourceID: r.ID, Entity: e.ID}
	case ChoiceCreate:
		e, err := linker.Create(ctx, who.Scope, r)
		if errors.Is(err, ErrEntityExists) {
			return failure(c, http.StatusConflict, "app_exists")
		}
		if err != nil {
			return failure(c, http.StatusUnprocessableEntity, "name")
		}
		link = Link{ResourceID: r.ID, Entity: e.ID}
	default:
		return failure(c, http.StatusUnprocessableEntity, "choice")
	}
	if err := linker.SetLink(ctx, who.Scope, product, link); errors.Is(err, ErrLinkedElsewhere) {
		return failure(c, http.StatusConflict, "linked_elsewhere")
	} else if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, NewLinkDTO(link))
}

// Disconnect godoc
// @Summary  Revoke the grant at urbangate and forget it; the links it reached stay, marked revoked
// @Tags     connect
// @Param    product  path  string  true  "product id"
// @Success  204
// @Failure  404  {object}  map[string]string
// @Failure  503  {object}  map[string]string
// @Router   /connections/{product} [delete]
// @ID       disconnectProduct
func (h *Handler) Disconnect(c echo.Context) error {
	who, err := h.cfg.Identify(c)
	if err != nil {
		return err
	}
	if err := h.grants.Disconnect(c.Request().Context(), who.Owner, c.Param("product")); err != nil {
		return h.productFailure(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
