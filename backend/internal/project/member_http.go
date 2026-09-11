// 本文件提供项目成员关系 HTTP 接口，写操作必须由项目角色中间件限制为项目管理员或系统管理员。
package project

import (
	"encoding/json"
	"errors"
	"net/http"

	"cmdb/internal/identity"
	"github.com/gin-gonic/gin"
)

// memberResponse 将成员关系与必要的公开身份组合，禁止序列化用户模型中的认证字段。
type memberResponse struct {
	Role        string `json:"role"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// memberCandidateResponse 是添加成员选择器允许读取的最小身份资料。
type memberCandidateResponse struct {
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
		response = append(response, newMemberResponse(&member))
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
		response = append(response, memberCandidateResponse{Username: user.Username, DisplayName: user.DisplayName})
	}
	c.JSON(http.StatusOK, response)
}

// AddMember 为公开用户名对应的用户授予项目内角色，未知身份使用稳定错误。
func (h *HTTPHandler) AddMember(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	var request struct {
		Username     string          `json:"username"`
		LegacyUserID json.RawMessage `json:"user_id"`
		Role         string          `json:"role"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.LegacyUserID) > 0 {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	member, err := h.service.AddMember(c.Request.Context(), projectID, request.Username, request.Role)
	if errors.Is(err, ErrInvalidMemberInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	if errors.Is(err, ErrMemberAlreadyExists) {
		writeProjectError(c, http.StatusConflict, "PROJECT_MEMBER_EXISTS", "项目成员已存在")
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
	c.JSON(http.StatusCreated, newMemberResponse(member))
}

// UpdateMemberRole 修改目标成员角色，不能通过该接口创建不存在的成员关系。
func (h *HTTPHandler) UpdateMemberRole(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	username, ok := memberUsernameFromPath(c)
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
	member, err := h.service.UpdateMemberRole(c.Request.Context(), projectID, username, request.Role)
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
	c.JSON(http.StatusOK, newMemberResponse(member))
}

// RemoveMember 移除指定项目成员关系，成员不存在时返回稳定的成员未找到响应。
func (h *HTTPHandler) RemoveMember(c *gin.Context) {
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	username, ok := memberUsernameFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
		return
	}
	claims, authenticated := currentUserClaims(c)
	if !authenticated {
		writeProjectError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份认证已失效")
		return
	}
	// 项目管理员不能删除自己的成员关系；系统管理员仍可执行全局纠正操作。
	if !isSystemAdmin(claims) && claims.Username == username {
		writeProjectError(c, http.StatusConflict, "PROJECT_MEMBER_SELF_REMOVE", "项目管理员不能移除自己")
		return
	}
	if err := h.service.RemoveMember(c.Request.Context(), projectID, username); errors.Is(err, ErrInvalidMemberInput) {
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

// memberUsernameFromPath 校验用户名格式；纯数字也是用户名，绝不回退为数据库 ID。
func memberUsernameFromPath(c *gin.Context) (string, bool) {
	username := c.Param("username")
	return username, identity.ValidUsername(username)
}

// newMemberResponse 统一成员写入和列表的公开身份，隐藏成员关系和用户内部主键。
func newMemberResponse(member *MemberRole) memberResponse {
	response := memberResponse{Role: member.Role}
	if member.User != nil {
		response.Username = member.User.Username
		response.DisplayName = member.User.DisplayName
	}
	return response
}
