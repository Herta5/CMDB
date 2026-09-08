package handler

import (
	"strconv"

	"github-cmdb/internal/middleware"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	svc *service.UserSvc
}

func NewUserHandler(svc *service.UserSvc) *UserHandler {
	return &UserHandler{svc: svc}
}

// Login authenticates a user and returns a JWT token.
func (h *UserHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	u, _, err := h.svc.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(401, gin.H{"code": -1, "message": err.Error()})
		return
	}

	token, err := middleware.GenerateToken(u.ID, u.Username, u.Roles, 24)
	if err != nil {
		response.InternalError(c, "failed to generate token")
		return
	}

	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": loginResponse(u, token)})
}

func loginResponse(user *model.User, token string) gin.H {
	return gin.H{
		"token":        token,
		"user_id":      user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"roles":        user.Roles,
	}
}

// List returns a paginated user list.
func (h *UserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	filter := repository.UserFilter{
		Username: c.Query("username"),
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
	}

	users, total, err := h.svc.List(filter)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Page(c, users, total, page, pageSize)
}

// Get returns a single user.
func (h *UserHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }

	u, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "user not found"); return }
	response.Success(c, u)
}

// Create creates a new user.
func (h *UserHandler) Create(c *gin.Context) {
	var req struct {
		Username    string   `json:"username" binding:"required"`
		Password    string   `json:"password" binding:"required"`
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		Phone       string   `json:"phone"`
		Roles       []string `json:"roles"`
		Departments []string `json:"departments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	u := &model.User{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		Roles:       req.Roles,
		Departments: req.Departments,
	}
	if err := u.SetPassword(req.Password); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	if err := h.svc.Create(u); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, u)
}

// Update updates an existing user.
func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }

	var req struct {
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		Phone       string   `json:"phone"`
		Roles       []string `json:"roles"`
		Departments []string `json:"departments"`
		Status      string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	u, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "user not found"); return }

	if req.DisplayName != "" { u.DisplayName = req.DisplayName }
	if req.Email != "" { u.Email = req.Email }
	if req.Phone != "" { u.Phone = req.Phone }
	if req.Roles != nil { u.Roles = req.Roles }
	if req.Departments != nil { u.Departments = req.Departments }
	if req.Status != "" { u.Status = req.Status }

	if err := h.svc.Update(u); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, u)
}

// Delete deletes a user.
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }

	if err := h.svc.Delete(id); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, nil)
}

// ResetPassword resets a user's password (admin only).
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }

	var req struct {
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.svc.ResetPassword(id, req.NewPassword); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, nil)
}

// ChangePassword allows a user to change their own password.
func (h *UserHandler) ChangePassword(c *gin.Context) {
	id := middleware.GetCurrentUserID(c)
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.svc.ChangePassword(id, req.OldPassword, req.NewPassword); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, nil)
}
