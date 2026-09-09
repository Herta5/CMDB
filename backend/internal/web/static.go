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

func Mount(r *gin.Engine, staticDir string) error {
	if staticDir == "" {
		return nil
	}

	root, err := filepath.Abs(staticDir)
	if err != nil {
		return fmt.Errorf("resolve static directory: %w", err)
	}
	indexPath := filepath.Join(root, "index.html")
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		return fmt.Errorf("static index unavailable at %s", indexPath)
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
		if strings.HasPrefix(requestPath, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
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
