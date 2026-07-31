package api

import (
	"context"

	fiberadaptor "github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	app *fiber.App
}

func New() *Server {
	return &Server{
		app: fiber.New(fiber.Config{DisableStartupMessage: true}),
	}
}

func (s *Server) mountRoutes() {
	s.app.Use(fiberrecover.New())

	// Prometheus metrics endpoint
	s.app.Get("/metrics", fiberadaptor.HTTPHandler(promhttp.Handler()))

	// Health check (chain status)
	s.app.Get("/health", func(c *fiber.Ctx) error {
		cr := checkRpc(c.Context())
		ok := cr.OK
		out := fiber.Map{"ok": ok, "chain": cr.OK}
		if cr.Epoch > 0 {
			out["epoch"] = cr.Epoch
		}
		code := fiber.StatusOK
		if !ok {
			code = fiber.StatusServiceUnavailable
		}
		return c.Status(code).JSON(out)
	})
}

func (s *Server) Start(addr string) error {
	s.mountRoutes()
	if addr == "" {
		addr = ":8080"
	}
	return s.app.Listen(addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.app.ShutdownWithContext(ctx)
}
