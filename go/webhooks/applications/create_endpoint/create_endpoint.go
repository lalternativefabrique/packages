package create_endpoint

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type Command struct {
	TenantID    string
	URL         string
	EventTypes  []string
	Description string
}

type Result struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Secret      string   `json:"secret"` // returned ONCE on creation; never again
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

func (h *Handler) Handle(ctx context.Context, cmd Command) (*Result, error) {
	if cmd.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}

	id := uuid.New().String()
	e := aggregate.NewEndpoint(id)
	if err := e.Create(cmd.TenantID, cmd.URL, cmd.EventTypes, cmd.Description, h.catalog); err != nil {
		return nil, err
	}
	if err := h.r.Save(ctx, e); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}

	return &Result{
		ID:          e.ID,
		URL:         e.URL,
		EventTypes:  e.EventTypes,
		Description: e.Description,
		Status:      string(e.Status),
		Secret:      e.Secret,
	}, nil
}

// ── HTTP controller ──────────────────────────────────────────────────────

type Controller struct{ h *Handler }

func NewController(h *Handler) *Controller { return &Controller{h: h} }

type CreateEndpointRequest struct {
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description,omitempty"`
}

// Handle godoc
// @Summary Create a webhook endpoint
// @Tags webhooks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateEndpointRequest true "Endpoint to create"
// @Success 201 {object} Result
// @Failure 400 {object} echo.HTTPError
// @Failure 401 {object} echo.HTTPError
// @Router /webhooks/endpoints [post]
// @ID createWebhookEndpoint
func (c *Controller) Handle(ec echo.Context) error {
	var req CreateEndpointRequest
	if err := ec.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	tenantID, _ := ec.Get("tenant_id").(string)

	res, err := c.h.Handle(ec.Request().Context(), Command{
		TenantID:    tenantID,
		URL:         req.URL,
		EventTypes:  req.EventTypes,
		Description: req.Description,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ec.JSON(http.StatusCreated, res)
}
