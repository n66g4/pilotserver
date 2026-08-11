package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillingStubReturnsIsPrime(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/billing/subscription")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body["is_prime"] {
		t.Fatalf("is_prime = %v, want true", body["is_prime"])
	}
}
