package routes

import (
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Engine struct{}
type Group struct{}
type Controller struct{ service *Service }
type Service struct{ db *gorm.DB }

func (*Engine) Group(string) *Group { return nil }
func (*Engine) GET(string, ...any)  {}
func (*Engine) POST(string, ...any) {}
func (*Group) GET(string, ...any)   {}
func (*Group) POST(string, ...any)  {}

func Register(engine *Engine, controller *Controller, dynamicPrefix string) {
	api := engine.Group("/api")
	api.GET("/items/:id", controller.List)
	api.POST(dynamicPrefix, controller.Create)
	engine.GET("/health", Health)
}

func (c *Controller) List(_ any) {
	c.service.Query()
}

func (c *Controller) Create(_ any) {
	c.service.db.Create(map[string]any{"name": "sample"})
}

func (s *Service) Query() {
	var rows []any
	s.db.Find(&rows)
	zap.NewNop().Info("query complete")
}

func Health(_ any) {}
