package get_endpoint

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type Query struct {
	TenantID string
	ID       string
}

type Handler struct{ r repository.EndpointReader }

func NewHandler(r repository.EndpointReader) *Handler { return &Handler{r: r} }

func (h *Handler) Handle(ctx context.Context, q Query) (*repository.EndpointView, error) {
	if q.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if q.ID == "" {
		return nil, errors.New("id is required")
	}
	v, err := h.r.Get(ctx, q.TenantID, q.ID)
	if err != nil {
		return nil, err
	}
	return v, nil
}

type Controller struct{ h *Handler }

func NewController(h *Handler) *Controller { return &Controller{h: h} }

// Handle godoc
// @Summary Get a webhook endpoint
// @Tags webhooks
// @Produce json
// @Security BearerAuth
// @Param id path string true "Endpoint ID"
// @Success 200 {object} repository.EndpointView
// @Failure 400 {object} echo.HTTPError
// @Failure 401 {object} echo.HTTPError
// @Failure 404 {object} echo.HTTPError
// @Router /webhooks/endpoints/{id} [get]
// @ID getWebhookEndpoint
func (c *Controller) Handle(ec echo.Context) error {
	tenantID, _ := ec.Get("tenant_id").(string)
	id := ec.Param("id")

	v, err := c.h.Handle(ec.Request().Context(), Query{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "endpoint not found")
		}
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ec.JSON(http.StatusOK, v)
}
