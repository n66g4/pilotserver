package replay

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTicketRouteAndSegmentRoundTrip(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)

	routeToken, expiresAt, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatalf("Issue(route) error = %v", err)
	}
	routeTicket, err := manager.Verify(routeToken)
	if err != nil {
		t.Fatalf("Verify(route) error = %v", err)
	}
	if routeTicket.DongleID != "dongle" || routeTicket.Route != "route" ||
		routeTicket.Mode != TicketModeRoute || routeTicket.Segment != nil ||
		!routeTicket.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("Verify(route) = %#v, expires %v", routeTicket, expiresAt)
	}

	segment := 7
	segmentToken, _, err := manager.Issue("dongle", "route", TicketModeSegment, &segment)
	if err != nil {
		t.Fatalf("Issue(segment) error = %v", err)
	}
	segmentTicket, err := manager.Verify(segmentToken)
	if err != nil {
		t.Fatalf("Verify(segment) error = %v", err)
	}
	if segmentTicket.Segment == nil || *segmentTicket.Segment != segment {
		t.Fatalf("Verify(segment).Segment = %v, want %d", segmentTicket.Segment, segment)
	}
}

func TestTicketIssueUsesUniqueJTI(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	first, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two issued tickets are identical")
	}
}

func TestTicketRejectsInvalidIssueScope(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	segment := 1
	tests := []struct {
		name    string
		mode    TicketMode
		segment *int
	}{
		{name: "route with segment", mode: TicketModeRoute, segment: &segment},
		{name: "segment without segment", mode: TicketModeSegment},
		{name: "unknown mode", mode: TicketMode("other")},
	}
	negative := -1
	tests = append(tests, struct {
		name    string
		mode    TicketMode
		segment *int
	}{name: "negative segment", mode: TicketModeSegment, segment: &negative})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := manager.Issue("dongle", "route", tt.mode, tt.segment); err == nil {
				t.Fatal("Issue() error = nil, want scope error")
			}
		})
	}
	if _, _, err := manager.Issue("../dongle", "route", TicketModeRoute, nil); err == nil {
		t.Fatal("Issue() accepted invalid dongle")
	}
}

func TestTicketVerifyRejectsWrongJWTKindAndScope(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	now := time.Now()
	base := jwt.MapClaims{
		"aud":    "pilotserver-media",
		"type":   "media",
		"dongle": "dongle",
		"route":  "route",
		"mode":   "route",
		"exp":    now.Add(time.Minute).Unix(),
		"iat":    now.Unix(),
		"jti":    "test-id",
	}

	tests := []struct {
		name   string
		method jwt.SigningMethod
		change func(jwt.MapClaims)
	}{
		{name: "administrator type", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["type"] = "admin" }},
		{name: "wrong audience", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["aud"] = "pilotserver-admin" }},
		{name: "wrong algorithm", method: jwt.SigningMethodHS384, change: func(jwt.MapClaims) {}},
		{name: "expired", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() }},
		{name: "unknown mode", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["mode"] = "other" }},
		{name: "route with segment", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["segment"] = 1 }},
		{name: "segment without segment", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) { c["mode"] = "segment" }},
		{name: "negative segment", method: jwt.SigningMethodHS256, change: func(c jwt.MapClaims) {
			c["mode"] = "segment"
			c["segment"] = -1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.MapClaims{}
			for key, value := range base {
				claims[key] = value
			}
			tt.change(claims)
			token, err := jwt.NewWithClaims(tt.method, claims).SignedString([]byte("secret"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Verify(token); err == nil {
				t.Fatal("Verify() error = nil, want rejection")
			}
		})
	}
}
