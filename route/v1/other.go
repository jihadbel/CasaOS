package v1

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func GetSearchResult(ctx echo.Context) error {
	return echo.NewHTTPError(http.StatusGone, "The legacy URL search proxy is disabled")
}
