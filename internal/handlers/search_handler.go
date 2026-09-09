package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/labstack/echo/v4"
)

type SearchHandler struct {
	searchService *services.SearchService
}

func NewSearchHandler(searchService *services.SearchService) *SearchHandler {
	return &SearchHandler{searchService: searchService}
}

func (h *SearchHandler) GlobalSearch(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	query := c.QueryParam("q")
	if query == "" {
		query = c.QueryParam("search")
	}

	results, err := h.searchService.GlobalSearch(accountID, query)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "SEARCH_ERROR", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, results)
}

func (h *SearchHandler) AdminSearch(c echo.Context) error {
	query := c.QueryParam("q")
	if query == "" {
		query = c.QueryParam("search")
	}

	results, err := h.searchService.AdminSearch(query)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "SEARCH_ERROR", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, results)
}
