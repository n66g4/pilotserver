package routepath

import "testing"

func TestParseSegmentFile(t *testing.T) {
	tests := []struct {
		name        string
		relPath     string
		wantRoute   string
		wantSegment string
		wantOK      bool
	}{
		{name: "two level zero", relPath: "00000010--2cbbf69c9f--0/qcamera.ts", wantRoute: "00000010--2cbbf69c9f", wantSegment: "0", wantOK: true},
		{name: "two level twelve", relPath: "route--with--separators--12/qlog.zst", wantRoute: "route--with--separators", wantSegment: "12", wantOK: true},
		{name: "two level one hundred sixteen", relPath: "route--116/qcamera.ts", wantRoute: "route", wantSegment: "116", wantOK: true},
		{name: "three level", relPath: "route--with--separators/12/qcamera.ts", wantRoute: "route--with--separators", wantSegment: "12", wantOK: true},
		{name: "boot", relPath: "boot/qlog.zst"},
		{name: "missing suffix", relPath: "route/qcamera.ts"},
		{name: "non decimal suffix", relPath: "route--abc/qcamera.ts"},
		{name: "empty route", relPath: "--12/qcamera.ts"},
		{name: "signed suffix", relPath: "route--+1/qcamera.ts"},
		{name: "extra level", relPath: "route/12/extra/qcamera.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseSegmentFile(tt.relPath)
			if ok != tt.wantOK {
				t.Fatalf("ParseSegmentFile(%q) ok = %v, want %v; got %+v", tt.relPath, ok, tt.wantOK, got)
			}
			if ok && (got.RouteName != tt.wantRoute || got.SegmentName != tt.wantSegment) {
				t.Fatalf("ParseSegmentFile(%q) = %+v, want route=%q segment=%q",
					tt.relPath, got, tt.wantRoute, tt.wantSegment)
			}
		})
	}
}
