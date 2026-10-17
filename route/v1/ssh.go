package v1

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

// Legacy terminal endpoints accepted passwords in URLs and are retired.
func PostSshLogin(ctx echo.Context) error {
	return echo.NewHTTPError(http.StatusGone, "The legacy SSH terminal is disabled")
}
func WsSsh(ctx echo.Context) error {
	return echo.NewHTTPError(http.StatusGone, "The legacy SSH terminal is disabled")
}
