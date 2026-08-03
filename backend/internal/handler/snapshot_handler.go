package handler

import (
	"encoding/json"
	"strconv"

	"github-cmdb/internal/collector"
	"github-cmdb/internal/repository"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

// SnapshotHandler handles config snapshot endpoints.
type SnapshotHandler struct {
	snapRepo *repository.ConfigSnapshotRepo
}

func NewSnapshotHandler(snapRepo *repository.ConfigSnapshotRepo) *SnapshotHandler {
	return &SnapshotHandler{snapRepo: snapRepo}
}

// List returns paginated snapshots for a given CI.
func (h *SnapshotHandler) List(c *gin.Context) {
	ciID, err := strconv.ParseUint(c.Query("ci_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "ci_id is required")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	snaps, total, err := h.snapRepo.List(ciID, page, size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Page(c, snaps, total, page, size)
}

// Get returns a single snapshot by ID.
func (h *SnapshotHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	snap, err := h.snapRepo.GetByID(id)
	if err != nil {
		response.NotFound(c, "snapshot not found")
		return
	}
	response.Success(c, snap)
}

// Diff compares two snapshots and returns the attribute-level diff.
func (h *SnapshotHandler) Diff(c *gin.Context) {
	fromID, err := strconv.ParseUint(c.Query("from"), 10, 64)
	if err != nil {
		response.BadRequest(c, "from (snapshot id) is required")
		return
	}
	toID, err := strconv.ParseUint(c.Query("to"), 10, 64)
	if err != nil {
		response.BadRequest(c, "to (snapshot id) is required")
		return
	}

	fromSnap, err := h.snapRepo.GetByID(fromID)
	if err != nil {
		response.NotFound(c, "from snapshot not found")
		return
	}
	toSnap, err := h.snapRepo.GetByID(toID)
	if err != nil {
		response.NotFound(c, "to snapshot not found")
		return
	}

	// Convert JSONMap to map[string]interface{} for diff
	fromAttrs := jsonMapToInterface(fromSnap.SnapshotData)
	toAttrs := jsonMapToInterface(toSnap.SnapshotData)

	result := collector.DiffAttributes(fromAttrs, toAttrs)
	response.Success(c, gin.H{
		"from_snapshot": gin.H{"id": fromSnap.ID, "created_at": fromSnap.CreatedAt},
		"to_snapshot":   gin.H{"id": toSnap.ID, "created_at": toSnap.CreatedAt},
		"diff":          result,
	})
}

func jsonMapToInterface(jm interface{}) map[string]interface{} {
	// JSONMap is defined in model; we serialize and deserialize to convert.
	b, err := json.Marshal(jm)
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	json.Unmarshal(b, &result)
	return result
}