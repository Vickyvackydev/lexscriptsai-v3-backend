package handlers

import (
	"net/http"
	"strings"

	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/labstack/echo/v4"
)

type RecordRoomHandler struct {
	recordRoomService *services.RecordRoomService
}

func NewRecordRoomHandler(recordRoomService *services.RecordRoomService) *RecordRoomHandler {
	return &RecordRoomHandler{
		recordRoomService: recordRoomService,
	}
}

func (h *RecordRoomHandler) HandleWebSocket(c echo.Context) error {
	roomKey := strings.TrimSpace(c.Param("roomKey"))
	if roomKey == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_ROOM_KEY", "Room key is required", nil)
	}

	role := strings.ToLower(c.QueryParam("role"))
	if role != "host" {
		role = "remote"
	}

	return h.recordRoomService.HandleWS(c.Response().Writer, c.Request(), roomKey, role)
}

type RemoteCommandRequest struct {
	Action  string                 `json:"action"`
	Payload map[string]interface{} `json:"payload"`
}

func (h *RecordRoomHandler) HandleCommand(c echo.Context) error {
	roomKey := strings.TrimSpace(c.Param("roomKey"))
	if roomKey == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_ROOM_KEY", "Room key is required", nil)
	}

	var req RemoteCommandRequest
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.Action) == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_REQUEST", "Action is required", nil)
	}

	if err := h.recordRoomService.DispatchCommand(roomKey, req.Action, req.Payload); err != nil {
		return response.Error(c, http.StatusBadRequest, "COMMAND_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"message": "Command dispatched to host successfully",
		"action":  req.Action,
	})
}

func (h *RecordRoomHandler) GetRoomInfo(c echo.Context) error {
	roomKey := strings.TrimSpace(c.Param("roomKey"))
	if roomKey == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_ROOM_KEY", "Room key is required", nil)
	}

	info, err := h.recordRoomService.GetRoomInfo(roomKey)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "INFO_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, info)
}
