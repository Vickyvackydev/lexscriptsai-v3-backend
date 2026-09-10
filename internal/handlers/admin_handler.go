package handlers

import (
	"io"
	"net/http"
	"os"
	"strconv"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type AdminHandler struct {
	accountService *services.AccountService
	auditService   *services.AuditService
}

func NewAdminHandler(accountService *services.AccountService, auditService *services.AuditService) *AdminHandler {
	return &AdminHandler{
		accountService: accountService,
		auditService:   auditService,
	}
}

func (h *AdminHandler) ListAccounts(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.QueryParam("pageSize"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	search := c.QueryParam("search")

	accounts, total, err := h.accountService.ListAccounts(page, pageSize, search)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}

	return response.Paginated(c, accounts, page, pageSize, total)
}

func (h *AdminHandler) GetAccount(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid account ID format", nil)
	}

	account, err := h.accountService.GetAccount(id)
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "Account not found", nil)
	}

	return response.Success(c, http.StatusOK, account)
}

func (h *AdminHandler) CreateAccount(c echo.Context) error {
	var input services.CreateAccountInput
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	actor := middleware.GetCurrentUser(c)
	account, err := h.accountService.CreateAccount(input, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, account)
}

func (h *AdminHandler) AddSubAccount(c echo.Context) error {
	accountIDStr := c.Param("id")
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid account ID", nil)
	}

	var input services.CreateSubAccount
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	actor := middleware.GetCurrentUser(c)
	user, err := h.accountService.AddSubAccount(accountID, input, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "ADD_MEMBER_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, user)
}

type UpdateStatusRequest struct {
	Status models.AccountStatus `json:"status"`
}

func (h *AdminHandler) UpdateUserStatus(c echo.Context) error {
	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid user ID", nil)
	}

	var req UpdateStatusRequest
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Status is required (active, disabled, suspended)", nil)
	}

	actor := middleware.GetCurrentUser(c)
	if err := h.accountService.UpdateUserStatus(userID, req.Status, actor); err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "User status updated successfully"})
}

func (h *AdminHandler) UpdateAccountStatus(c echo.Context) error {
	accountIDStr := c.Param("id")
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid account ID", nil)
	}

	var req UpdateStatusRequest
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Status is required", nil)
	}

	actor := middleware.GetCurrentUser(c)
	if err := h.accountService.UpdateAccountStatus(accountID, req.Status, actor); err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Account status updated successfully"})
}

func (h *AdminHandler) GetStats(c echo.Context) error {
	stats, err := h.accountService.GetAdminStats()
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "STATS_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, stats)
}

func (h *AdminHandler) GetLogs(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.QueryParam("pageSize"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}

	logs, total, err := h.auditService.GetLogs(nil, page, pageSize)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "LOGS_FAILED", err.Error(), nil)
	}

	return response.Paginated(c, logs, page, pageSize, total)
}

func (h *AdminHandler) ClearLogs(c echo.Context) error {
	if err := h.auditService.ClearLogs(nil); err != nil {
		return response.Error(c, http.StatusInternalServerError, "CLEAR_LOGS_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, map[string]string{"message": "Audit logs cleared successfully"})
}

func (h *AdminHandler) UpdateAccount(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid account ID", nil)
	}

	var input services.UpdateAccountInput
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	actor := middleware.GetCurrentUser(c)
	account, err := h.accountService.UpdateAccount(id, input, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "UPDATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, account)
}

func (h *AdminHandler) DeleteAccount(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid account ID", nil)
	}

	actor := middleware.GetCurrentUser(c)
	if err := h.accountService.DeleteAccount(id, actor); err != nil {
		return response.Error(c, http.StatusBadRequest, "DELETE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Account deleted successfully"})
}

type AdminUpdatePasswordRequest struct {
	Password string `json:"password"`
}

func (h *AdminHandler) AdminUpdatePassword(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid account ID", nil)
	}

	var req AdminUpdatePasswordRequest
	if err := c.Bind(&req); err != nil || len(req.Password) < 8 {
		return response.Error(c, http.StatusBadRequest, "INVALID_PASSWORD", "Password must be at least 8 characters long", nil)
	}

	actor := middleware.GetCurrentUser(c)
	if err := h.accountService.AdminUpdatePassword(id, req.Password, actor); err != nil {
		return response.Error(c, http.StatusBadRequest, "UPDATE_PASSWORD_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Password updated successfully and notification email dispatched to user"})
}

func (h *AdminHandler) ListNotifications(c echo.Context) error {
	notifs, err := h.accountService.ListAdminNotifications()
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "LIST_NOTIFS_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, notifs)
}

func (h *AdminHandler) MarkNotificationRead(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid notification ID", nil)
	}

	if err := h.accountService.MarkNotificationRead(id); err != nil {
		return response.Error(c, http.StatusInternalServerError, "MARK_READ_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Notification marked as read"})
}

func (h *AdminHandler) ResolveNotification(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid notification ID", nil)
	}

	if err := h.accountService.ResolveNotification(id); err != nil {
		return response.Error(c, http.StatusInternalServerError, "RESOLVE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Notification resolved"})
}

func (h *AdminHandler) UpdateCookies(c echo.Context) error {
	file, err := c.FormFile("cookies")
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "FILE_REQUIRED", "Cookies file is required", nil)
	}

	src, err := file.Open()
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "OPEN_FAILED", "Failed to open cookies file", nil)
	}
	defer src.Close()

	cookiePath := os.Getenv("YOUTUBE_COOKIES_PATH")
	if cookiePath == "" {
		cookiePath = "cookies.txt"
	}

	dst, err := os.Create(cookiePath)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "SAVE_FAILED", "Failed to save cookies file", nil)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		return response.Error(c, http.StatusInternalServerError, "COPY_FAILED", "Failed to write cookies file", nil)
	}

	actor := middleware.GetCurrentUser(c)
	if actor != nil && h.auditService != nil {
		h.auditService.Log(nil, "", actor.ID, actor.Name, "admin_cookies_updated", "system", "", "Updated YouTube cookies file", "", "")
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "YouTube cookies updated successfully"})
}
