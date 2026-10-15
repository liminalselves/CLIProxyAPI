package multiuser

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

//go:embed panel/management.html
var panelFiles embed.FS

type panelAsset struct {
	raw    []byte
	zipped []byte
	etag   string
}

var (
	panelAssetOnce sync.Once
	panelAssetVal  panelAsset
)

// loadPanelAsset reads the embedded panel once and precomputes the gzipped
// payload and a content-derived ETag. The single-file panel is several MB,
// so repeat visits should revalidate (304) instead of re-downloading.
func loadPanelAsset() panelAsset {
	panelAssetOnce.Do(func() {
		data, err := panelFiles.ReadFile("panel/management.html")
		if err != nil {
			return
		}
		sum := sha256.Sum256(data)
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		if _, errWrite := writer.Write(data); errWrite == nil {
			if errClose := writer.Close(); errClose == nil {
				panelAssetVal.zipped = buf.Bytes()
			}
		}
		panelAssetVal.raw = data
		panelAssetVal.etag = `"` + hex.EncodeToString(sum[:16]) + `"`
	})
	return panelAssetVal
}

func Panel(c *gin.Context) {
	asset := loadPanelAsset()
	if asset.raw == nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	// no-cache allows storing but forces revalidation; unchanged panels get a
	// cheap 304 while redeploys change the ETag and ship the new build.
	c.Header("Cache-Control", "no-cache")
	c.Header("ETag", asset.etag)
	if strings.TrimSpace(c.GetHeader("If-None-Match")) == asset.etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if len(asset.zipped) > 0 && strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")
		c.Data(http.StatusOK, "text/html; charset=utf-8", asset.zipped)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", asset.raw)
}
