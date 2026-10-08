package multiuser

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed panel/management.html
var panelFiles embed.FS

func Panel(c *gin.Context) {
	data, err := panelFiles.ReadFile("panel/management.html")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
