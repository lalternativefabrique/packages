package rotate_secret

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type Command struct {
	TenantID string
	ID       string
}

type Result struct {
	ID     string `json:"id"`
	Secret string `json:"secret"` // returned ONCE on rotation
}

type Handler struct {
	r repository.EndpointRepository
}

func NewHandler(r repository.EndpointRepository) *Handler { return &Handler{r: r} }

func (h *Handler) Handle(ctx context.Context, cmd Command) (*Result, error) {
	if cmd.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if cmd.ID == "" {
		return nil, errors.New("id is required")
	}

	e, err := h.r.Load(ctx, cmd.ID)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	if e.Version == 0 {
		return nil, repository.ErrNotFound
	}
	if e.TenantID != cmd.TenantID {
		return nil, repository.ErrNotFound
	}

	secret, err := e.RotateSecret()
	if err != nil {
		return nil, err
	}
	if err := h.r.Save(ctx, e); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return &Result{ID: e.ID, Secret: secret}, nil
}

// ── HTTP controller ──────────────────────────────────────────────────────

type Controller struct{ h *Handler }

func NewController(h *Handler) *Controller { return &Controller{h: h} }

// Handle godoc
// @Summary Rotate a webhook endpoint's signing secret
// @Tags webhooks
// @Produce json
// @Security BearerAuth
// @Param id path string true "Endpoint ID"
// @Success 200 {object} Result
// @Failure 400 {object} echo.HTTPError
// @Failure 401 {object} echo.HTTPError
// @Failure 404 {object} echo.HTTPError
// @Router /webhooks/endpoints/{id}/rotate-secret [post]
// @ID rotateWebhookSecret
func (c *Controller) Handle(ec echo.Context) error {
	tenantID, _ := ec.Get("tenant_id").(string)
	id := ec.Param("id")

	res, err := c.h.Handle(ec.Request().Context(), Command{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "endpoint not found")
		}
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ec.JSON(http.StatusOK, res)
}
