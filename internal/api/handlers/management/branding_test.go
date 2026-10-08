package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestBrandingHandlers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if errWrite := os.WriteFile(path, []byte("config-version: 8\n"), 0600); errWrite != nil {
		t.Fatalf("write: %v", errWrite)
	}
	cfg, errLoad := config.LoadConfig(path)
	if errLoad != nil {
		t.Fatalf("load: %v", errLoad)
	}
	h := &Handler{cfg: cfg, configFilePath: path}
	router := gin.New()
	router.GET("/v8/branding", h.GetPublicBranding)
	router.PUT("/v8/management/branding", h.PutBranding)

	get := func() string {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v8/branding", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET branding: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	put := func(body string, wantStatus int) {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodPut, "/v8/management/branding", strings.NewReader(body)),
		)
		if recorder.Code != wantStatus {
			t.Fatalf("PUT branding %q: status=%d body=%s", body, recorder.Code, recorder.Body.String())
		}
	}

	if got := get(); !strings.Contains(got, `"site-name":""`) {
		t.Fatalf("initial branding = %s", got)
	}

	put(`{"site-name":"My Site","site-subtitle":"Custom subtitle","site-logo":"https://example.com/logo.png"}`, http.StatusOK)
	for _, want := range []string{`"site-name":"My Site"`, `"site-subtitle":"Custom subtitle"`, `"site-logo":"https://example.com/logo.png"`} {
		if got := get(); !strings.Contains(got, want) {
			t.Fatalf("updated branding = %s, want %s", got, want)
		}
	}
	if h.cfg.Branding.SiteName != "My Site" {
		t.Fatalf("in-memory branding not updated: %+v", h.cfg.Branding)
	}

	// The branding section must survive the v8 save path and reload.
	loaded, errReload := config.LoadConfig(path)
	if errReload != nil {
		t.Fatalf("reload: %v", errReload)
	}
	if loaded.Branding.SiteName != "My Site" || loaded.Branding.SiteSubtitle != "Custom subtitle" {
		t.Fatalf("persisted branding differs: %+v", loaded.Branding)
	}

	put(`{"site-name":"x","site-logo":"javascript:alert(1)"}`, http.StatusUnprocessableEntity)
	put(`{"site-name":"`+strings.Repeat("a", 101)+`"}`, http.StatusUnprocessableEntity)
	if h.cfg.Branding.SiteName != "My Site" {
		t.Fatalf("rejected write mutated branding: %+v", h.cfg.Branding)
	}

	put(`{"site-name":"","site-subtitle":"","site-logo":""}`, http.StatusOK)
	if got := get(); !strings.Contains(got, `"site-name":""`) {
		t.Fatalf("reset branding = %s", got)
	}
}
