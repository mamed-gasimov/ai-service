package server

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// HealthChecker is implemented by components that can report their health.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// PingFunc adapts a no-arg function (e.g. rabbitmq Ping) to HealthChecker.
type PingFunc func() error

func (f PingFunc) Ping(_ context.Context) error { return f() }

func New(minio HealthChecker, rabbitmq HealthChecker) *echo.Echo {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.GET("/health", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
		defer cancel()

		status := map[string]string{
			"status":   "ok",
			"minio":    "ok",
			"rabbitmq": "ok",
		}
		httpStatus := http.StatusOK

		if err := minio.Ping(ctx); err != nil {
			status["status"] = "degraded"
			status["minio"] = err.Error()
			httpStatus = http.StatusServiceUnavailable
		}

		if err := rabbitmq.Ping(ctx); err != nil {
			status["status"] = "degraded"
			status["rabbitmq"] = err.Error()
			httpStatus = http.StatusServiceUnavailable
		}

		return c.JSON(httpStatus, status)
	})

	return e
}
