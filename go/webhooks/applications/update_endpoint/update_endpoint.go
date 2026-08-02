package update_endpoint

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type Command struct {
	TenantID    string
	ID          string
	URL         string
	EventTypes  []string
	Description string
	Disabled    bool
}

type Handler struct {
	r       repository.EndpointRepository
	catalog events.Catalog
}

// NewHandler takes the catalog of public event types this product publishes.
// It is service configuration, never request data: a caller that could name its
// own valid types would defeat the validation entirely.
func NewHandler(r repository.EndpointRepository, catalog events.Catalog) *Handler {
	return &Handler{r: r, catalog: catalog}
}

func (h *Handler) Handle(ctx context.Context, cmd Command) error {
	if cmd.TenantID == "" {
		return errors.New("tenant_id is required")
	}
	if cmd.ID == "" {
		return errors.New("id is required")
	}

	e, err := h.r.Load(ctx, cmd.ID)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	if e.Version == 0 {
		return repository.ErrNotFound
	}
	if e.TenantID != cmd.TenantID {
		return repository.ErrNotFound // hide existence from other tenants
	}

	if err := e.Update(cmd.URL, cmd.EventTypes, cmd.Description, cmd.Disabled, h.catalog); err != nil {
		return err
	}
	if err := h.r.Save(ctx, e); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// ── HTTP controller ──────────────────────────────────────────────────────

type Controller struct{ h *Handler }

func NewController(h *Handler) *Controller { return &Controller{h: h} }

type UpdateEndpointRequest struct {
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description,omitempty"`
	Disabled    bool     `json:"disabled"`
}

// Handle godoc
// @Summary Update a webhook endpoint
// @Tags webhooks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Endpoint ID"
// @Param request body UpdateEndpointRequest true "Updated fields"
// @Success 204 "No Content"
// @Failure 400 {object} echo.HTTPError
// @Failure 401 {object} echo.HTTPError
// @Failure 404 {object} echo.HTTPError
// @Router /webhooks/endpoints/{id} [patch]
// @ID updateWebhookEndpoint
func (c *Controller) Handle(ec echo.Context) error {
	var req UpdateEndpointRequest
	if err := ec.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	tenantID, _ := ec.Get("tenant_id").(string)
	id := ec.Param("id")

	err := c.h.Handle(ec.Request().Context(), Command{
		TenantID:    tenantID,
		ID:          id,
		URL:         req.URL,
		EventTypes:  req.EventTypes,
		Description: req.Description,
		Disabled:    req.Disabled,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "endpoint not found")
		}
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ec.NoContent(http.StatusNoContent)
}
