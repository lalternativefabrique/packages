package list_endpoints

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type Query struct {
	TenantID string
	Limit    int
	Offset   int
}

type Result struct {
	Items []repository.EndpointView `json:"items"`
}

type Handler struct{ r repository.EndpointReader }

func NewHandler(r repository.EndpointReader) *Handler { return &Handler{r: r} }

func (h *Handler) Handle(ctx context.Context, q Query) (*Result, error) {
	if q.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	items, err := h.r.List(ctx, q.TenantID, q.Limit, q.Offset)
	if err != nil {
		return nil, err
	}
	return &Result{Items: items}, nil
}

type Controller struct{ h *Handler }

func NewController(h *Handler) *Controller { return &Controller{h: h} }

// Handle godoc
// @Summary List webhook endpoints
// @Tags webhooks
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Page size"
// @Param offset query int false "Offset"
// @Success 200 {object} Result
// @Failure 400 {object} echo.HTTPError
// @Failure 401 {object} echo.HTTPError
// @Router /webhooks/endpoints [get]
// @ID listWebhookEndpoints
func (c *Controller) Handle(ec echo.Context) error {
	tenantID, _ := ec.Get("tenant_id").(string)
	limit, _ := strconv.Atoi(ec.QueryParam("limit"))
	offset, _ := strconv.Atoi(ec.QueryParam("offset"))

	res, err := c.h.Handle(ec.Request().Context(), Query{TenantID: tenantID, Limit: limit, Offset: offset})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return ec.JSON(http.StatusOK, res)
}
