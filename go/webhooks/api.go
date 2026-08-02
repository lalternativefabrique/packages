package webhooks

import "github.com/labstack/echo/v4"

// RegisterRoutes mounts the webhooks management API. All endpoints require
// the standard tenant auth middleware applied on the parent group.
func (s *Service) RegisterRoutes(g *echo.Group) {
	if s.createCtl != nil {
		g.POST("/webhooks/endpoints", s.createCtl.Handle)
	}
	if s.listCtl != nil {
		g.GET("/webhooks/endpoints", s.listCtl.Handle)
	}
	if s.getCtl != nil {
		g.GET("/webhooks/endpoints/:id", s.getCtl.Handle)
	}
	if s.updateCtl != nil {
		g.PATCH("/webhooks/endpoints/:id", s.updateCtl.Handle)
	}
	if s.deleteCtl != nil {
		g.DELETE("/webhooks/endpoints/:id", s.deleteCtl.Handle)
	}
	if s.rotateCtl != nil {
		g.POST("/webhooks/endpoints/:id/rotate-secret", s.rotateCtl.Handle)
	}
}
