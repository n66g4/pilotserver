package replay

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBuildPlaylistSingleSegment(t *testing.T) {
	playlist, err := BuildPlaylist(MediaTicket{Mode: TicketModeRoute}, []Segment{{
		Number:         2,
		Duration:       1.2345,
		QCameraRelPath: "route/2/qcamera.ts",
	}})
	if err != nil {
		t.Fatalf("BuildPlaylist() error = %v", err)
	}
	want := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-PLAYLIST-TYPE:VOD\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-TARGETDURATION:2\n" +
		"#EXTINF:1.234,\n" +
		"2.ts\n" +
		"#EXT-X-ENDLIST\n"
	if playlist != want {
		t.Fatalf("BuildPlaylist() =\n%s\nwant:\n%s", playlist, want)
	}
}

func TestBuildPlaylistSortsWithoutMutatingAndAddsDiscontinuities(t *testing.T) {
	segments := []Segment{
		{Number: 10, Duration: 2.1, QCameraRelPath: "route/10/qcamera.ts"},
		{Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts"},
		{Number: 2, Duration: 3, QCameraRelPath: "route/2/qcamera.ts"},
	}
	before := append([]Segment(nil), segments...)

	playlist, err := BuildPlaylist(MediaTicket{Mode: TicketModeRoute}, segments)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(segments, before) {
		t.Fatalf("BuildPlaylist() mutated input: %#v", segments)
	}
	if strings.Index(playlist, "\n0.ts\n") > strings.Index(playlist, "\n2.ts\n") ||
		strings.Index(playlist, "\n2.ts\n") > strings.Index(playlist, "\n10.ts\n") {
		t.Fatalf("playlist is not numerically sorted:\n%s", playlist)
	}
	if strings.Count(playlist, "#EXT-X-DISCONTINUITY") != 2 {
		t.Fatalf("discontinuity count = %d, want 2", strings.Count(playlist, "#EXT-X-DISCONTINUITY"))
	}
	if !strings.Contains(playlist, "#EXT-X-TARGETDURATION:3\n") {
		t.Fatalf("playlist target duration is wrong:\n%s", playlist)
	}
}

func TestBuildPlaylistFiltersMissingQCameraAndSegmentScope(t *testing.T) {
	number := 2
	playlist, err := BuildPlaylist(MediaTicket{
		Mode:    TicketModeSegment,
		Segment: &number,
	}, []Segment{
		{Number: 0, Duration: 1},
		{Number: 1, Duration: 1, QCameraRelPath: "route/1/qcamera.ts"},
		{Number: 2, Duration: 2, QCameraRelPath: "route/2/qcamera.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(playlist, "0.ts") || strings.Contains(playlist, "1.ts") ||
		!strings.Contains(playlist, "\n2.ts\n") {
		t.Fatalf("playlist violates segment scope:\n%s", playlist)
	}
}

func TestBuildPlaylistReportsNoPlayableSegments(t *testing.T) {
	number := 2
	tests := []struct {
		name     string
		ticket   MediaTicket
		segments []Segment
	}{
		{name: "no qcamera", ticket: MediaTicket{Mode: TicketModeRoute}, segments: []Segment{{Number: 0, Duration: 1}}},
		{name: "selected segment absent", ticket: MediaTicket{Mode: TicketModeSegment, Segment: &number}, segments: []Segment{{
			Number: 1, Duration: 1, QCameraRelPath: "route/1/qcamera.ts",
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildPlaylist(tt.ticket, tt.segments)
			if !errors.Is(err, ErrNoPlayableSegments) {
				t.Fatalf("BuildPlaylist() error = %v, want ErrNoPlayableSegments", err)
			}
		})
	}
}

func TestBuildPlaylistRejectsInvalidDuration(t *testing.T) {
	for _, duration := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			_, err := BuildPlaylist(MediaTicket{Mode: TicketModeRoute}, []Segment{{
				Number: 0, Duration: duration, QCameraRelPath: "route/0/qcamera.ts",
			}})
			if err == nil {
				t.Fatalf("BuildPlaylist() accepted duration %v", duration)
			}
		})
	}
}
