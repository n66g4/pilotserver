package maps

import (
	"net/http"
)

var emptyJSON = []byte(`{}`)

func Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/maps/{path...}", serveEmpty)
	mux.HandleFunc("GET /maps/{path...}", serveEmpty)
}

func serveEmpty(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(emptyJSON)
}
