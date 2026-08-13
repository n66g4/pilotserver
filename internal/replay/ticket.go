package replay

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TicketMode string

const (
	TicketModeRoute   TicketMode = "route"
	TicketModeSegment TicketMode = "segment"
)

type MediaTicket struct {
	DongleID  string
	Route     string
	Mode      TicketMode
	Segment   *int
	ExpiresAt time.Time
}

type TicketManager struct {
	secret []byte
	ttl    time.Duration
}

type mediaClaims struct {
	DongleID string     `json:"dongle"`
	Route    string     `json:"route"`
	Mode     TicketMode `json:"mode"`
	Segment  *int       `json:"segment,omitempty"`
	Type     string     `json:"type"`
	jwt.RegisteredClaims
}

func NewTicketManager(secret string, ttl time.Duration) *TicketManager {
	return &TicketManager{secret: []byte(secret), ttl: ttl}
}

func (m *TicketManager) Issue(dongleID, route string, mode TicketMode, segment *int) (string, time.Time, error) {
	if err := validateTicket(dongleID, route, mode, segment); err != nil {
		return "", time.Time{}, err
	}

	jti, err := randomJTI()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate ticket ID: %v", err)
	}
	now := jwt.NewNumericDate(time.Now().UTC()).Time
	expiresAt := jwt.NewNumericDate(now.Add(m.ttl)).Time
	claims := mediaClaims{
		DongleID: dongleID,
		Route:    route,
		Mode:     mode,
		Segment:  segment,
		Type:     "media",
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"pilotserver-media"},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign media ticket: %v", err)
	}
	return token, expiresAt, nil
}

func (m *TicketManager) Verify(token string) (MediaTicket, error) {
	claims := new(mediaClaims)
	parsed, err := jwt.ParseWithClaims(token, claims, func(parsed *jwt.Token) (any, error) {
		if parsed.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience("pilotserver-media"),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt())
	if err != nil || !parsed.Valid {
		return MediaTicket{}, fmt.Errorf("invalid media ticket")
	}
	if claims.Type != "media" ||
		len(claims.Audience) != 1 || claims.Audience[0] != "pilotserver-media" ||
		claims.ExpiresAt == nil || claims.IssuedAt == nil || claims.ID == "" {
		return MediaTicket{}, fmt.Errorf("invalid media ticket claims")
	}
	if err := validateTicket(claims.DongleID, claims.Route, claims.Mode, claims.Segment); err != nil {
		return MediaTicket{}, fmt.Errorf("invalid media ticket claims")
	}
	return MediaTicket{
		DongleID:  claims.DongleID,
		Route:     claims.Route,
		Mode:      claims.Mode,
		Segment:   claims.Segment,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}

func validateTicket(dongleID, route string, mode TicketMode, segment *int) error {
	if err := validatePathComponent("dongle ID", dongleID); err != nil {
		return err
	}
	if err := validatePathComponent("route", route); err != nil {
		return err
	}
	switch mode {
	case TicketModeRoute:
		if segment != nil {
			return fmt.Errorf("route ticket cannot select a segment")
		}
	case TicketModeSegment:
		if segment == nil || *segment < 0 {
			return fmt.Errorf("segment ticket requires a non-negative segment")
		}
	default:
		return fmt.Errorf("invalid ticket mode")
	}
	return nil
}

func randomJTI() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
