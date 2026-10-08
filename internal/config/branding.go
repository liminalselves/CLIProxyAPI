package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// BrandingConfig holds custom site identity shown across the management panel,
// replacing the default product branding when set.
type BrandingConfig struct {
	// SiteName replaces the default panel product name in the sidebar, login page and title.
	SiteName string `yaml:"site-name" json:"site-name"`
	// SiteSubtitle replaces the default sidebar subtitle under the site name.
	SiteSubtitle string `yaml:"site-subtitle" json:"site-subtitle"`
	// SiteLogo is a data URL or http(s) URL replacing the default panel logo and favicon.
	SiteLogo string `yaml:"site-logo" json:"site-logo"`
}

const (
	maxBrandingNameLength     = 100
	maxBrandingSubtitleLength = 200
	maxBrandingLogoLength     = 2 << 21 // ~4MB of characters, roughly a 3MB embedded image
)

func (c BrandingConfig) Validate() error {
	if utf8.RuneCountInString(c.SiteName) > maxBrandingNameLength {
		return fmt.Errorf("branding.site-name must be at most %d characters", maxBrandingNameLength)
	}
	if utf8.RuneCountInString(c.SiteSubtitle) > maxBrandingSubtitleLength {
		return fmt.Errorf("branding.site-subtitle must be at most %d characters", maxBrandingSubtitleLength)
	}
	logo := strings.TrimSpace(c.SiteLogo)
	if logo == "" {
		return nil
	}
	if len(logo) > maxBrandingLogoLength {
		return fmt.Errorf("branding.site-logo is too large; keep it under 4MB")
	}
	if !strings.HasPrefix(logo, "data:image/") && !strings.HasPrefix(logo, "http://") && !strings.HasPrefix(logo, "https://") {
		return fmt.Errorf("branding.site-logo must be a data:image/ or http(s) URL")
	}
	return nil
}
