package replay

import (
	"math"
	"reflect"
	"strconv"
	"testing"

	"pilotserver/internal/store"
)

func TestBuildSegmentsAggregatesRecognizedFiles(t *testing.T) {
	files := []store.Segment{
		{RouteName: "route--name", SegmentName: "10", RelPath: "route--name/10/qcamera.ts"},
		{RouteName: "route--name", SegmentName: "2", RelPath: "route--name/2/qlog.zst"},
		{RouteName: "route--name", SegmentName: "2", RelPath: "route--name/2/qcamera.ts"},
		{RouteName: "route--name", SegmentName: "0", RelPath: "route--name/0/qlog.zst"},
		{RouteName: "route--name", SegmentName: "3", RelPath: "route--name/3/rlog.zst"},
	}

	got, err := BuildSegments("route--name", files)
	if err != nil {
		t.Fatalf("BuildSegments() error = %v", err)
	}
	if len(got) != 11 {
		t.Fatalf("BuildSegments() length = %d, want 11: %#v", len(got), got)
	}
	if got[0].QlogRelPath == "" || got[2].QCameraRelPath == "" ||
		got[2].QlogRelPath == "" || got[10].QCameraRelPath == "" {
		t.Fatalf("recognized files were not aggregated: %#v", got)
	}
	for number, segment := range got {
		if segment.Number != number {
			t.Fatalf("segment[%d].Number = %d", number, segment.Number)
		}
		if number != 0 && number != 2 && number != 10 &&
			(segment.QCameraRelPath != "" || segment.QlogRelPath != "") {
			t.Fatalf("gap segment %d contains media: %#v", number, segment)
		}
	}
}

func TestBuildSegmentsAddsMissingGapPlaceholder(t *testing.T) {
	got, err := BuildSegments("route", []store.Segment{
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qcamera.ts"},
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{
		{Number: 0, Duration: 60, DurationEstimated: true, QCameraRelPath: "route/0/qcamera.ts"},
		{Number: 1, Duration: 60, DurationEstimated: true},
		{Number: 2, Duration: 60, DurationEstimated: true, QCameraRelPath: "route/2/qcamera.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildSegments() = %#v, want %#v", got, want)
	}
}

func TestBuildSegmentsAggregatesTwoAndThreeLevelPaths(t *testing.T) {
	got, err := BuildSegments("route--with--parts", []store.Segment{
		{RouteName: "route--with--parts", SegmentName: "12", RelPath: "route--with--parts--12/qlog.zst"},
		{RouteName: "route--with--parts", SegmentName: "0", RelPath: "route--with--parts--0/qcamera.ts"},
		{RouteName: "route--with--parts", SegmentName: "0", RelPath: "route--with--parts/0/qlog.zst"},
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/bootlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 13 {
		t.Fatalf("segments = %+v", got)
	}
	if got[0].QCameraRelPath != "route--with--parts--0/qcamera.ts" ||
		got[0].QlogRelPath != "route--with--parts/0/qlog.zst" ||
		got[12].QlogRelPath != "route--with--parts--12/qlog.zst" {
		t.Fatalf("mixed path aggregation = %+v", got)
	}
}

func TestBuildSegmentsIgnoresUnsupportedFilesBeforeMetadataValidation(t *testing.T) {
	got, err := BuildSegments("boot", []store.Segment{
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/qlog.zst"},
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/qlog.zst.uploading"},
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/crash"},
	})
	if err != nil {
		t.Fatalf("unsupported boot files caused replay failure: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("segments = %+v, want empty", got)
	}
}

func TestBuildSegmentsRejectsMalformedRecognizedFileWithNumericMetadata(t *testing.T) {
	_, err := BuildSegments("boot", []store.Segment{{
		RouteName: "boot", SegmentName: "0", RelPath: "boot/qlog.zst",
	}})
	if err == nil {
		t.Fatal("BuildSegments accepted malformed qlog path with numeric segment metadata")
	}
}

func TestBuildSegmentsRejectsUnboundedGap(t *testing.T) {
	_, err := BuildSegments("route", []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "1000000", RelPath: "route/1000000/qcamera.ts"},
	})
	if err == nil {
		t.Fatal("BuildSegments() accepted unbounded segment span")
	}
}

func TestBuildSegmentsHandlesMaxIntSegment(t *testing.T) {
	number := strconv.Itoa(math.MaxInt)
	got, err := BuildSegments("route", []store.Segment{{
		RouteName: "route", SegmentName: number,
		RelPath: "route/" + number + "/qcamera.ts",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != math.MaxInt ||
		got[0].QCameraRelPath == "" {
		t.Fatalf("BuildSegments() = %#v", got)
	}
}

func TestBuildSegmentsRejectsInvalidSegmentName(t *testing.T) {
	for _, segmentName := range []string{"", "-1", "abc"} {
		t.Run(segmentName, func(t *testing.T) {
			_, err := BuildSegments("route", []store.Segment{{
				RouteName:   "route",
				SegmentName: segmentName,
				RelPath:     "route/" + segmentName + "/qcamera.ts",
			}})
			if err == nil {
				t.Fatal("BuildSegments() error = nil, want validation error")
			}
		})
	}
}

func TestBuildSegmentsRejectsInvalidRelativePath(t *testing.T) {
	tests := []struct {
		name        string
		routeName   string
		segmentName string
		relPath     string
	}{
		{name: "route metadata mismatch", routeName: "other", segmentName: "1", relPath: "route/1/qcamera.ts"},
		{name: "segment metadata mismatch", routeName: "route", segmentName: "2", relPath: "route/1/qcamera.ts"},
		{name: "absolute", routeName: "route", segmentName: "1", relPath: "/route/1/qcamera.ts"},
		{name: "dot component", routeName: "route", segmentName: "1", relPath: "route/./qcamera.ts"},
		{name: "parent component", routeName: "route", segmentName: "1", relPath: "route/../qcamera.ts"},
		{name: "backslash", routeName: "route", segmentName: "1", relPath: `route\1\qcamera.ts`},
		{name: "extra directory", routeName: "route", segmentName: "1", relPath: "route/1/extra/qcamera.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildSegments("route", []store.Segment{{
				RouteName:   tt.routeName,
				SegmentName: tt.segmentName,
				RelPath:     tt.relPath,
			}})
			if err == nil {
				t.Fatal("BuildSegments() error = nil, want validation error")
			}
		})
	}
}

func TestBuildSegmentsRejectsConflictingTargetPaths(t *testing.T) {
	_, err := BuildSegments("route", []store.Segment{
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qcamera.ts"},
		{RouteName: "route", SegmentName: "01", RelPath: "route/01/qcamera.ts"},
	})
	if err == nil {
		t.Fatal("BuildSegments() error = nil, want conflict error")
	}
}
