package routepath

import (
	"path"
	"strings"
)

type SegmentFile struct {
	RouteName   string
	SegmentName string
	Filename    string
}

func ParseSegmentFile(relPath string) (SegmentFile, bool) {
	if relPath == "" || path.IsAbs(relPath) || path.Clean(relPath) != relPath ||
		strings.Contains(relPath, `\`) {
		return SegmentFile{}, false
	}
	parts := strings.Split(relPath, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return SegmentFile{}, false
		}
	}

	var parsed SegmentFile
	switch len(parts) {
	case 2:
		split := strings.LastIndex(parts[0], "--")
		if split <= 0 {
			return SegmentFile{}, false
		}
		parsed = SegmentFile{
			RouteName:   parts[0][:split],
			SegmentName: parts[0][split+2:],
			Filename:    parts[1],
		}
	case 3:
		parsed = SegmentFile{
			RouteName:   parts[0],
			SegmentName: parts[1],
			Filename:    parts[2],
		}
	default:
		return SegmentFile{}, false
	}
	if !isDecimal(parsed.SegmentName) {
		return SegmentFile{}, false
	}
	return parsed, true
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
