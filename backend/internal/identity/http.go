// 本文件提供身份域的公开 HTTP 接口，所有响应均排除密码和密码哈希。
package identity

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HTTPHandler 将身份服务适配为 HTTP 处理器，不承担 JWT 解析职责。
type HTTPHandler struct {
	service *Service
}

// NewHTTPHandler 创建身份接口处理器。
func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

// Login 处理用户名和密码登录，并以稳定错误响应避免暴露账户存在性或内部存储状态。
func (h *HTTPHandler) Login(c *gin.Context) {
	var request struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "请求格式错误")
		return
	}

	user, token, err := h.service.Login(c.Request.Context(), request.Username, request.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		writeError(c, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_SERVICE_UNAVAILABLE", "认证服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, loginResponse{Token: token, User: toPublicUser(user)})
}

// Me 返回数据库中的当前公开身份资料；令牌解析和声明校验由 HTTP 服务中间件完成。
func (h *HTTPHandler) Me(c *gin.Context, claims UserClaims) {
	user, err := h.service.CurrentUser(c.Request.Context(), claims)
	if errors.Is(err, ErrAuthenticatedUserNotFound) {
		writeError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份认证已失效")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_SERVICE_UNAVAILABLE", "认证服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, toPublicUser(user))
}

// loginResponse 仅在成功登录时返回会话令牌与安全的公开用户资料。
type loginResponse struct {
	Token string     `json:"token"`
	User  publicUser `json:"user"`
}

// publicUser 明确列出可暴露给客户端的用户字段，避免模型新增敏感字段时被意外序列化。
type publicUser struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	GlobalRole  string `json:"global_role"`
	Status      string `json:"status"`
}

// toPublicUser 将领域用户转为客户端可见的最小身份资料。
func toPublicUser(user *User) publicUser {
	return publicUser{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		GlobalRole:  user.GlobalRole,
		Status:      user.Status,
	}
}

// errorResponse 固定认证错误的 JSON 结构，调用方可据稳定错误码进行处理。
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError 输出已脱敏的认证错误，禁止把数据库、JWT 或密码错误原样暴露给客户端。
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorResponse{Code: code, Message: message})
}
