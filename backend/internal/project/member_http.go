// 本文件提供项目成员关系 HTTP 接口，写操作必须由项目角色中间件限制为项目管理员或系统管理员。
package project

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// memberResponse 将成员关系与必要的公开身份组合，禁止序列化用户模型中的认证字段。
type memberResponse struct {
	ID          uint64 `json:"id"`
	UserID      uint64 `json:"user_id"`
	Role        string `json:"role"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// memberCandidateResponse 是添加成员选择器允许读取的最小身份资料。
type memberCandidateResponse struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// ListMembers 返回已获项目读取权限的用户可见成员关系。
func (h *HTTPHandler) ListMembers(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	members, err := h.service.ListMembers(c.Request.Context(), projectID)
	if errors.Is(err, ErrInvalidMemberInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	response := make([]memberResponse, 0, len(members))
	for _, member := range members {
		item := memberResponse{ID: member.ID, UserID: member.UserID, Role: member.Role}
		if member.User != nil {
			item.Username = member.User.Username
			item.DisplayName = member.User.DisplayName
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// ListMemberCandidates 返回项目管理员可选择的启用用户，不包含邮箱、状态或全局角色。
func (h *HTTPHandler) ListMemberCandidates(c *gin.Context) {
	users, err := h.service.ListMemberCandidates(c.Request.Context())
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	response := make([]memberCandidateResponse, 0, len(users))
	for _, user := range users {
		response = append(response, memberCandidateResponse{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName})
	}
	c.JSON(http.StatusOK, response)
}

// AddMember 为目标用户授予项目内角色，目标用户是否存在由数据库外键保证且内部错误不对外暴露。
func (h *HTTPHandler) AddMember(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	var request struct {
		UserID uint64 `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	member, err := h.service.AddMember(c.Request.Context(), projectID, request.UserID, request.Role)
	if errors.Is(err, ErrInvalidMemberInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	if errors.Is(err, ErrMemberAlreadyExists) {
		writeProjectError(c, http.StatusConflict, "PROJECT_MEMBER_EXISTS", "项目成员已存在")
		return
	}
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.JSON(http.StatusCreated, member)
}

// UpdateMemberRole 修改目标成员角色，不能通过该接口创建不存在的成员关系。
func (h *HTTPHandler) UpdateMemberRole(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	userID, ok := memberUserIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	var request struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	member, err := h.service.UpdateMemberRole(c.Request.Context(), projectID, userID, request.Role)
	if errors.Is(err, ErrInvalidMemberInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	if errors.Is(err, ErrMemberNotFound) {
		writeProjectError(c, http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "项目成员不存在")
		return
	}
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, member)
}

// RemoveMember 移除指定项目成员关系，成员不存在时返回稳定的成员未找到响应。
func (h *HTTPHandler) RemoveMember(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	userID, ok := memberUserIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	if err := h.service.RemoveMember(c.Request.Context(), projectID, userID); errors.Is(err, ErrInvalidMemberInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	} else if errors.Is(err, ErrMemberNotFound) {
		writeProjectError(c, http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "项目成员不存在")
		return
	} else if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.Status(http.StatusNoContent)
}

// memberUserIDFromPath 严格解析成员用户标识，拒绝零值和非数字以防错误修改成员关系。
func memberUserIDFromPath(c *gin.Context) (uint64, bool) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	return userID, err == nil && userID > 0
}
