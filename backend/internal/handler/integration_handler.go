package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github-cmdb/internal/eventbus"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type IntegrationHandler struct {
	ciRepo    *repository.CIInstanceRepo
	typeRepo  *repository.CITypeRepo
	auditRepo *repository.AuditRepo
	changeSvc *service.ChangeSvc
}

func NewIntegrationHandler(ciRepo *repository.CIInstanceRepo, typeRepo *repository.CITypeRepo, auditRepo *repository.AuditRepo, changeSvc *service.ChangeSvc) *IntegrationHandler {
	return &IntegrationHandler{ciRepo: ciRepo, typeRepo: typeRepo, auditRepo: auditRepo, changeSvc: changeSvc}
}

// PrometheusTargets returns file_sd format
func (h *IntegrationHandler) PrometheusTargets(c *gin.Context) {
	instances, _, err := h.ciRepo.List(repository.CIInstanceFilter{
		Status: "active", Page: 1, PageSize: 10000,
	})
	if err != nil { response.InternalError(c, err.Error()); return }

	type sdTarget struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}

	grouped := make(map[string][]string)
	for _, ci := range instances {
		if ci.IPAddress == nil || *ci.IPAddress == "" { continue }
		key := ci.Name
		if ci.CIType != nil { key = ci.CIType.DisplayName }
		grouped[key] = append(grouped[key], *ci.IPAddress+":9100")
	}

	var result []sdTarget
	for group, targets := range grouped {
		result = append(result, sdTarget{
			Targets: targets,
			Labels:  map[string]string{"job": "cmdb-sd", "group": group},
		})
	}
	if result == nil { result = []sdTarget{} }
	c.JSON(http.StatusOK, result)
}

// AnsibleInventory returns dynamic inventory JSON
func (h *IntegrationHandler) AnsibleInventory(c *gin.Context) {
	instances, _, err := h.ciRepo.List(repository.CIInstanceFilter{
		Status: "active", Page: 1, PageSize: 10000,
	})
	if err != nil { response.InternalError(c, err.Error()); return }

	type hostVars map[string]interface{}
	inventory := gin.H{
		"_meta": gin.H{"hostvars": gin.H{}},
		"all":   gin.H{"children": []string{}},
	}

	groups := make(map[string][]string)
	for _, ci := range instances {
		if ci.IPAddress == nil || *ci.IPAddress == "" { continue }
		groupName := "ungrouped"
		if ci.CIType != nil { groupName = ci.CIType.Name }
		groups[groupName] = append(groups[groupName], *ci.IPAddress)

		// Host vars
		vars := gin.H{
			"ansible_host": *ci.IPAddress,
			"cmdb_id":      ci.ID,
			"cmdb_name":    ci.Name,
		}
		if ci.Owner != nil { vars["owner"] = *ci.Owner }
		inventory["_meta"].(gin.H)["hostvars"].(gin.H)[*ci.IPAddress] = vars
	}

	children := make([]string, 0, len(groups))
	for name, hosts := range groups {
		inventory[name] = gin.H{"hosts": hosts}
		children = append(children, name)
	}
	inventory["all"] = gin.H{"children": children}

	c.JSON(http.StatusOK, inventory)
}

// AlertmanagerWebhook receives alerts from Prometheus Alertmanager
func (h *IntegrationHandler) AlertmanagerWebhook(c *gin.Context) {
	body, _ := c.GetRawData()
	record := &model.WebhookRecord{
		Source:    "alertmanager",
		EventType: "alert",
		Payload:   string(body),
	}

	// Extract headers
	headerBytes, _ := json.Marshal(c.Request.Header)
	record.Headers = string(headerBytes)

	_ = h.auditRepo.CreateWebhook(record)

	// Try to extract alert info and link to CI
	var payload struct {
		Alerts []struct {
			Labels map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			Status string `json:"status"`
		} `json:"alerts"`
	}
	if json.Unmarshal(body, &payload) == nil {
		for _, alert := range payload.Alerts {
			eventbus.Default.Publish(eventbus.Event{
				Type:      eventbus.EventType("alert.received"),
				Timestamp: time.Now(),
				Source:    "alertmanager",
				Payload: map[string]interface{}{
					"status":      alert.Status,
					"labels":      alert.Labels,
					"annotations": alert.Annotations,
				},
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

// GenericWebhook receives webhooks from any external system
func (h *IntegrationHandler) GenericWebhook(c *gin.Context) {
	source := c.Query("source")
	if source == "" { source = "unknown" }

	body, _ := c.GetRawData()
	record := &model.WebhookRecord{
		Source:    source,
		EventType: c.GetHeader("X-Event-Type"),
		Payload:   string(body),
	}
	headerBytes, _ := json.Marshal(c.Request.Header)
	record.Headers = string(headerBytes)

	_ = h.auditRepo.CreateWebhook(record)

	eventbus.Default.Publish(eventbus.Event{
		Type:      eventbus.EventType("webhook." + source),
		Timestamp: time.Now(),
		Source:    source,
		Payload:   map[string]interface{}{"body": string(body), "headers": c.Request.Header},
	})

	c.JSON(http.StatusOK, gin.H{"status": "received", "id": record.ID})
}

// WebhookHistory returns recent webhook records
func (h *IntegrationHandler) WebhookHistory(c *gin.Context) {
	records, total, err := h.auditRepo.ListWebhooks(1, 50)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Page(c, records, total, 1, 50)
}