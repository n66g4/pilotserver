package replay

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var ErrNoPlayableSegments = errors.New("no playable segments")

func BuildPlaylist(ticket MediaTicket, segments []Segment) (string, error) {
	playable := make([]Segment, 0, len(segments))
	for _, segment := range segments {
		if segment.QCameraRelPath == "" {
			continue
		}
		switch ticket.Mode {
		case TicketModeRoute:
		case TicketModeSegment:
			if ticket.Segment == nil {
				return "", fmt.Errorf("segment ticket has no segment")
			}
			if segment.Number != *ticket.Segment {
				continue
			}
		default:
			return "", fmt.Errorf("invalid ticket mode")
		}
		if segment.Duration <= 0 || math.IsNaN(segment.Duration) || math.IsInf(segment.Duration, 0) {
			return "", fmt.Errorf("segment %d has invalid duration", segment.Number)
		}
		playable = append(playable, segment)
	}
	if len(playable) == 0 {
		return "", ErrNoPlayableSegments
	}
	sort.Slice(playable, func(i, j int) bool {
		return playable[i].Number < playable[j].Number
	})

	targetDuration := 0
	for _, segment := range playable {
		targetDuration = max(targetDuration, int(math.Ceil(segment.Duration)))
	}

	var playlist strings.Builder
	fmt.Fprintln(&playlist, "#EXTM3U")
	fmt.Fprintln(&playlist, "#EXT-X-VERSION:3")
	fmt.Fprintln(&playlist, "#EXT-X-PLAYLIST-TYPE:VOD")
	fmt.Fprintln(&playlist, "#EXT-X-MEDIA-SEQUENCE:0")
	fmt.Fprintf(&playlist, "#EXT-X-TARGETDURATION:%d\n", targetDuration)
	for i, segment := range playable {
		if i > 0 {
			fmt.Fprintln(&playlist, "#EXT-X-DISCONTINUITY")
		}
		fmt.Fprintf(&playlist, "#EXTINF:%.3f,\n%d.ts\n", segment.Duration, segment.Number)
	}
	fmt.Fprintln(&playlist, "#EXT-X-ENDLIST")
	return playlist.String(), nil
}
