package api

import (
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSGRelayDevPanelRequiresExplicitConfiguration(t *testing.T) {
	t.Setenv("CSGCLAW_CSGRELAY_DEV_PANEL", "")
	router := chi.NewRouter()
	(&Handler{}).registerCSGRelayDevRoutes(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/csgrelay-dev", nil))
	if w.Code != 404 {
		t.Fatalf("disabled panel returned %d", w.Code)
	}
	t.Setenv("CSGCLAW_CSGRELAY_DEV_PANEL", "1")
	t.Setenv("CSGCLAW_CSGRELAY_URL", "http://127.0.0.1:28080")
	t.Setenv("CSGCLAW_CSGRELAY_TOKEN", "")
	router = chi.NewRouter()
	(&Handler{}).registerCSGRelayDevRoutes(router)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/csgrelay-dev", nil))
	if w.Code != 404 {
		t.Fatal("panel enabled without server-side token")
	}
}
func TestCSGRelayDevAPIAuthenticatesAndNeverReturnsUpstreamToken(t *testing.T) {
	t.Setenv("CSGCLAW_DATA_DIR", t.TempDir())
	t.Setenv("CSGCLAW_CSGRELAY_DEV_PANEL", "1")
	t.Setenv("CSGCLAW_CSGRELAY_URL", "http://127.0.0.1:28080")
	t.Setenv("CSGCLAW_CSGRELAY_TOKEN", "upstream-secret")
	h := &Handler{serverAccessToken: "local-secret"}
	router := chi.NewRouter()
	h.registerCSGRelayDevRoutes(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/csgrelay-dev/info", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/v1/csgrelay-dev/info", nil)
	r.Header.Set("Authorization", "Bearer local-secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "upstream-secret") {
		t.Fatalf("info leaked credential or failed: %d", w.Code)
	}
}
