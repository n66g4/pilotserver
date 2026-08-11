package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminUIRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mountAdminUI(mux)

	for _, path := range []string{"/admin/", "/admin/index.html"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "<title>Pilotserver Admin</title>") {
			t.Errorf("%s did not serve admin page", path)
		}
	}
}
