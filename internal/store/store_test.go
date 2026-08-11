package store_test

import (
	"testing"
	"time"

	"pilotserver/internal/store"
)

func TestUpsertAndGetDevice(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d := store.Device{
		DongleID:     "testdongle001",
		PublicKeyPEM: "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----",
		Alias:        "car1",
		CreatedAt:    time.Now().Unix(),
	}
	if err := s.UpsertDevice(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDevice("testdongle001")
	if err != nil {
		t.Fatal(err)
	}
	if got.DongleID != d.DongleID || got.Alias != "car1" {
		t.Fatalf("got %+v", got)
	}
}
