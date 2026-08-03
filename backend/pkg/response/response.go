package response

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type PageData struct {
	Items      interface{} `json:"items"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

func Page(c *gin.Context, data interface{}, total int64, page, pageSize int) {
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 { totalPages++ }
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: PageData{
		Items: data, Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages,
	}})
}

func Error(c *gin.Context, httpStatus int, message string) {
	c.JSON(httpStatus, Response{Code: -1, Message: message})
}

func BadRequest(c *gin.Context, message string)   { Error(c, http.StatusBadRequest, message) }
func NotFound(c *gin.Context, message string)      { Error(c, http.StatusNotFound, message) }
func InternalError(c *gin.Context, message string)  { Error(c, http.StatusInternalServerError, message) }