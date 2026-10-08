package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// GetPublicBranding returns the site branding overrides shown before
// authentication (login page, browser title). It exposes no secrets.
func (h *Handler) GetPublicBranding(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, config.BrandingConfig{})
		return
	}
	c.JSON(http.StatusOK, h.cfg.Branding)
}

// PutBranding replaces the site branding overrides.
func (h *Handler) PutBranding(c *gin.Context) {
	var body config.BrandingConfig
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body", "message": errBind.Error()})
		return
	}
	body.SiteName = strings.TrimSpace(body.SiteName)
	body.SiteSubtitle = strings.TrimSpace(body.SiteSubtitle)
	body.SiteLogo = strings.TrimSpace(body.SiteLogo)
	if errValidate := body.Validate(); errValidate != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_branding", "message": errValidate.Error()})
		return
	}
	h.cfg.Branding = body
	h.persist(c)
}
