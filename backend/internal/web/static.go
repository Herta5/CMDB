// Package web 在单体服务中提供前端静态资源和页面刷新回退，保持 API 边界独立。
package web

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Mount 挂载构建产物；未配置目录用于独立前端开发，已配置但缺失首页则拒绝启动。
func Mount(r *gin.Engine, staticDir string) error {
	if staticDir == "" {
		return nil
	}

	root, err := filepath.Abs(staticDir)
	if err != nil {
		return fmt.Errorf("解析静态目录失败：%w", err)
	}
	indexPath := filepath.Join(root, "index.html")
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		return fmt.Errorf("静态首页不可用：%s", indexPath)
	}

	r.GET("/assets/*filepath", func(c *gin.Context) {
		filePath, ok := resolveFile(filepath.Join(root, "assets"), c.Param("filepath"))
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.File(filePath)
	})
	r.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		// 未知 API 必须明确失败，不能由单页应用回退掩盖客户端路径错误。
		if strings.HasPrefix(requestPath, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "接口不存在"})
			return
		}
		if filePath, ok := resolveFile(root, requestPath); ok {
			if filePath == indexPath {
				c.Header("Cache-Control", "no-cache")
			}
			c.File(filePath)
			return
		}
		if path.Ext(requestPath) != "" {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(indexPath)
	})
	return nil
}

// resolveFile 将客户端路径限制在构建目录内，只返回存在的普通文件。
func resolveFile(root, requestPath string) (string, bool) {
	relative := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	contained, err := filepath.Rel(root, candidate)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Stat(candidate)
	return candidate, err == nil && !info.IsDir()
}
