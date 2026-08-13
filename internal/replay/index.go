package replay

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pilotserver/internal/routepath"
	"pilotserver/internal/store"
)

const (
	defaultSegmentDuration = 60
	maxSegmentTimelineSpan = 10_000
)

func BuildSegments(route string, files []store.Segment) ([]Segment, error) {
	segments := make(map[int]Segment)

	for _, file := range files {
		filename := file.RelPath
		if slash := strings.LastIndexAny(filename, `/\`); slash >= 0 {
			filename = filename[slash+1:]
		}
		if filename != "qcamera.ts" && filename != "qlog.zst" {
			continue
		}
		if _, err := parseSegmentNumber(file.SegmentName); err != nil {
			if _, ok := routepath.ParseSegmentFile(file.RelPath); !ok {
				parts := strings.Split(file.RelPath, "/")
				if len(parts) == 2 && parts[0] == file.RouteName &&
					file.SegmentName == file.RouteName {
					continue
				}
			}
		}
		number, filename, err := validateSegmentFile(route, file)
		if err != nil {
			return nil, err
		}

		segment, ok := segments[number]
		if !ok {
			segment = Segment{
				Number:            number,
				Duration:          defaultSegmentDuration,
				DurationEstimated: true,
			}
		}

		switch filename {
		case "qcamera.ts":
			if segment.QCameraRelPath != "" && segment.QCameraRelPath != file.RelPath {
				return nil, fmt.Errorf("segment %d has conflicting qcamera paths", number)
			}
			segment.QCameraRelPath = file.RelPath
		case "qlog.zst":
			if segment.QlogRelPath != "" && segment.QlogRelPath != file.RelPath {
				return nil, fmt.Errorf("segment %d has conflicting qlog paths", number)
			}
			segment.QlogRelPath = file.RelPath
		}
		segments[number] = segment
	}

	numbers := make([]int, 0, len(segments))
	for number := range segments {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	if len(numbers) == 0 {
		return nil, nil
	}
	if numbers[len(numbers)-1]-numbers[0] > maxSegmentTimelineSpan {
		return nil, fmt.Errorf("segment timeline span exceeds %d", maxSegmentTimelineSpan)
	}

	count := numbers[len(numbers)-1] - numbers[0] + 1
	result := make([]Segment, 0, count)
	for offset := 0; offset < count; offset++ {
		number := numbers[0] + offset
		segment, ok := segments[number]
		if !ok {
			segment = Segment{
				Number:            number,
				Duration:          defaultSegmentDuration,
				DurationEstimated: true,
			}
		}
		result = append(result, segment)
	}
	return result, nil
}

func validateSegmentFile(route string, file store.Segment) (int, string, error) {
	if file.RouteName != route {
		return 0, "", fmt.Errorf("route metadata %q does not match %q", file.RouteName, route)
	}

	number, err := parseSegmentNumber(file.SegmentName)
	if err != nil {
		return 0, "", err
	}

	parsed, ok := routepath.ParseSegmentFile(file.RelPath)
	if !ok {
		return 0, "", fmt.Errorf("invalid segment relative path %q", file.RelPath)
	}
	if parsed.RouteName != file.RouteName || parsed.SegmentName != file.SegmentName {
		return 0, "", fmt.Errorf("relative path %q does not match segment metadata", file.RelPath)
	}
	return number, parsed.Filename, nil
}

func parseSegmentNumber(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("segment name is empty")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("segment name %q is not a non-negative decimal integer", value)
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid segment name %q: %v", value, err)
	}
	return number, nil
}
