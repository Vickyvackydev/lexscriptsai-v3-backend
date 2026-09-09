package response

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Response struct {
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Error      *APIError   `json:"error,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalItems int64 `json:"totalItems"`
	TotalPages int   `json:"totalPages"`
}

func Success(c echo.Context, statusCode int, data interface{}) error {
	return c.JSON(statusCode, Response{
		Success: true,
		Data:    data,
	})
}

func Paginated(c echo.Context, data interface{}, page int, pageSize int, totalItems int64) error {
	totalPages := 0
	if pageSize > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}
	return c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
		Pagination: &Pagination{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: totalItems,
			TotalPages: totalPages,
		},
	})
}

func Error(c echo.Context, statusCode int, code string, message string, details interface{}) error {
	return c.JSON(statusCode, Response{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}
