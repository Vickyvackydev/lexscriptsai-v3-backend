package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type LocationHandler struct {
	locationService *services.LocationService
}

func NewLocationHandler(locationService *services.LocationService) *LocationHandler {
	return &LocationHandler{locationService: locationService}
}

func (h *LocationHandler) ListLocations(c echo.Context) error {
	locations, err := h.locationService.ListLocations()
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, locations)
}

func (h *LocationHandler) CreateLocation(c echo.Context) error {
	var input services.CreateLocationInput
	if err := c.Bind(&input); err != nil || input.Name == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Location name is required", nil)
	}

	location, err := h.locationService.CreateLocation(input)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusCreated, location)
}

type UpdateLocationRequest struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

func (h *LocationHandler) UpdateLocation(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid location ID", nil)
	}

	var req UpdateLocationRequest
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Location name is required", nil)
	}

	location, err := h.locationService.UpdateLocation(id, req.Name, req.State)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, location)
}

func (h *LocationHandler) DeleteLocation(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid location ID", nil)
	}

	if err := h.locationService.DeleteLocation(id); err != nil {
		return response.Error(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, map[string]string{"message": "Location deleted successfully"})
}
