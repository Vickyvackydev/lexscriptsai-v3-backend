package middleware

import (
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			res := c.Response()
			res.Header().Set("X-Content-Type-Options", "nosniff")
			res.Header().Set("X-Frame-Options", "DENY")
			res.Header().Set("X-XSS-Protection", "1; mode=block")
			res.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			res.Header().Set("Content-Security-Policy", "default-src 'self'")
			res.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			return next(c)
		}
	}
}

func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			reqID := c.Request().Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = uuid.New().String()
			}
			c.Set("requestId", reqID)
			c.Response().Header().Set("X-Request-ID", reqID)
			return next(c)
		}
	}
}

func StructuredLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			latency := time.Since(start)

			req := c.Request()
			res := c.Response()

			userID := c.Get(ContextKeyUserID)
			accountID := c.Get(ContextKeyAccountID)
			reqID := c.Get("requestId")

			log.Printf("[HTTP] id=%v method=%s path=%s status=%d latency=%v user=%v account=%v\n",
				reqID, req.Method, req.URL.Path, res.Status, latency, userID, accountID)

			return err
		}
	}
}
