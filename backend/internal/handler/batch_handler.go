package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type BatchHandler struct {
	ciSvc *service.CIInstanceSvc
}

func NewBatchHandler(ciSvc *service.CIInstanceSvc) *BatchHandler {
	return &BatchHandler{ciSvc: ciSvc}
}

func (h *BatchHandler) ImportCSV(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil { response.BadRequest(c, "missing file"); return }
	f, err := file.Open()
	if err != nil { response.InternalError(c, err.Error()); return }
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil { response.BadRequest(c, "invalid CSV: "+err.Error()); return }
	if len(records) < 2 { response.BadRequest(c, "CSV must have header + at least 1 row"); return }

	header := records[0]
	typeIDIdx, nameIdx := -1, -1
	for i, h := range header {
		switch strings.TrimSpace(h) {
		case "ci_type_id": typeIDIdx = i
		case "name": nameIdx = i
		}
	}
	if typeIDIdx == -1 || nameIdx == -1 {
		response.BadRequest(c, "CSV must contain ci_type_id and name columns"); return
	}

	created, failed := 0, 0
	var errs []string
	for i, row := range records[1:] {
		if len(row) < len(header) { continue }
		attrs := model.JSONMap{}
		for j, h := range header {
			h = strings.TrimSpace(h)
			v := strings.TrimSpace(row[j])
			if v == "" { continue }
			switch h {
			case "ci_type_id", "name": continue
			default: attrs[h] = v
			}
		}
		ci := &model.CIInstance{
			CITypeID:   parseUint(row[typeIDIdx]),
			Name:       strings.TrimSpace(row[nameIdx]),
			Attributes: attrs,
			Source:     "import",
		}
		if err := h.ciSvc.Create(ci); err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("row %d: %s", i+1, err.Error()))
		} else {
			created++
		}
	}
	response.Success(c, gin.H{"created": created, "failed": failed, "errors": errs})
}

func (h *BatchHandler) ExportCSV(c *gin.Context) {
	var filter struct {
		CITypeID *uint64 `form:"ci_type_id"`
		Status   string  `form:"status"`
	}
	if err := c.ShouldBindQuery(&filter); err != nil { response.BadRequest(c, err.Error()); return }

	instances, _, err := h.ciSvc.List(repository.CIInstanceFilter{
		CITypeID: filter.CITypeID, Status: filter.Status, Page: 1, PageSize: 10000,
	})
	if err != nil { response.InternalError(c, err.Error()); return }

	attrKeys := make(map[string]bool)
	for _, ci := range instances {
		if ci.Attributes != nil {
			for k := range ci.Attributes { attrKeys[k] = true }
		}
	}
	keys := make([]string, 0, len(attrKeys))
	for k := range attrKeys { keys = append(keys, k) }

	header := []string{"ci_code", "ci_type_id", "name", "status", "ip_address", "owner"}
	header = append(header, keys...)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=cmdb_export_%s.csv", time.Now().Format("20060102")))
	c.Writer.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(c.Writer)
	w.Write(header)
	for _, ci := range instances {
		row := []string{ci.CICode, fmt.Sprintf("%d", ci.CITypeID), ci.Name, ci.Status, strVal(ci.IPAddress), strVal(ci.Owner)}
		for _, k := range keys {
			v := ""
			if ci.Attributes != nil {
				if val, ok := ci.Attributes[k]; ok {
					switch vv := val.(type) {
					case string: v = vv
					default: b, _ := json.Marshal(vv); v = string(b)
					}
				}
			}
			row = append(row, v)
		}
		w.Write(row)
	}
	w.Flush()
}

func parseUint(s string) uint64 {
	var v uint64
	fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	return v
}

func strVal(s *string) string { if s == nil { return "" }; return *s }