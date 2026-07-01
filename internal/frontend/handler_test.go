package frontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestVersionedAssetsAreCacheable(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/app-AbCdEf12.js", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	cacheControl := recorder.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable asset cache", cacheControl)
	}
	if recorder.Header().Get("Clear-Site-Data") != "" {
		t.Fatal("versioned asset should not clear browser cache")
	}
}

func TestIndexDoesNotClearBrowserCache(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/workflow/123", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if recorder.Header().Get("Clear-Site-Data") != "" {
		t.Fatal("index should not force all assets to be downloaded again")
	}
}

func TestMissingAssetReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/missing-AbCdEf12.js", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "index") {
		t.Fatal("missing asset should not fall back to index.html")
	}
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := NewHandler(Config{
		EmbedFS: fstest.MapFS{
			"index.html":             {Data: []byte("<html>index</html>")},
			"assets/app-AbCdEf12.js": {Data: []byte("console.log('ok')")},
		},
		CSP: CSPConfig{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
