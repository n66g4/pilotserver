package billing

import (
	"net/http"
)

var primeJSON = []byte(`{"is_prime":true}`)

func Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/billing/{path...}", servePrime)
	mux.HandleFunc("POST /v1/billing/{path...}", servePrime)
	mux.HandleFunc("GET /v1/subscription/{path...}", servePrime)
	mux.HandleFunc("GET /v1/prime/{path...}", servePrime)
}

func servePrime(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(primeJSON)
}
