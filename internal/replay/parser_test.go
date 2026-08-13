package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"capnproto.org/go/capnp/v3"
	"github.com/klauspost/compress/zstd"

	"pilotserver/internal/replay/cereal"
)

const testSecond = uint64(1_000_000_000)

func TestParserMixedStreamExtraction(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(7, 12*testSecond),
		qRoadEvent(8, 9*testSecond),
		carStateEvent(10*testSecond+500_000_000, 12.5),
		selfdriveStateEvent(11*testSecond, cereal.SelfdriveState_OpenpilotState_enabled, true, true, "alert one", "alert two"),
		gpsEventWithFix(10*testSecond+250_000_000, true, false, 0, 0, 0, 0, 4_294_967.5),
		unknownEvent(10*testSecond),
		qRoadEvent(7, 10*testSecond),
		gpsEvent(11*testSecond+500_000_000, false, 23.1291, 113.2644, 15.5, 90.25, 2.5),
		qRoadEvent(7, 11*testSecond),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 7)
	if err != nil {
		t.Fatal(err)
	}

	if got.Segment != 7 || got.Duration != 3 || got.DurationEstimated || got.VideoStartMonoTime != 10*testSecond {
		t.Fatalf("unexpected timing metadata: %+v", got)
	}
	if !reflect.DeepEqual(got.Speeds, []SpeedSample{{Time: 0.5, Value: 12.5}}) {
		t.Fatalf("Speeds = %+v", got.Speeds)
	}
	if !reflect.DeepEqual(got.Controls, []ControlSample{{
		Time: 1, Enabled: true, Active: true, State: "enabled",
		AlertText1: "alert one", AlertText2: "alert two",
	}}) {
		t.Fatalf("Controls = %+v", got.Controls)
	}
	if !reflect.DeepEqual(got.GPS, []GPSSample{{
		Time: 1.5, Latitude: 23.1291, Longitude: 113.2644, Speed: 15.5,
		BearingDeg: 90.25, HorizontalAccuracy: 2.5,
	}}) {
		t.Fatalf("GPS = %+v", got.GPS)
	}
}

func TestParserFiltersInvalidExternalGPSAndPrefersValidExternal(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(3, 10*testSecond),
		qRoadEvent(3, 11*testSecond),
		gpsEventWithFix(10*testSecond, true, false, 23, 113, 0, 0, 1),
		gpsEvent(10*testSecond, true, math.NaN(), 113, 0, 0, 1),
		gpsEvent(10*testSecond, true, 23, math.Inf(1), 0, 0, 1),
		gpsEvent(10*testSecond, true, 91, 113, 0, 0, 1),
		gpsEvent(10*testSecond, true, 23, 181, 0, 0, 1),
		gpsEvent(10*testSecond, true, 0, 0, 0, 0, 1),
		gpsEvent(10*testSecond, false, 23.1291, 113.2644, 0, 0, 1),
		gpsEvent(10*testSecond+500_000_000, true, 22.5431, 114.0579, 3, 45, -1),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []GPSSample{{
		Time: 0.5, Latitude: 22.5431, Longitude: 114.0579,
		Speed: 3, BearingDeg: 45, HorizontalAccuracy: -1,
	}}
	if !reflect.DeepEqual(got.GPS, want) {
		t.Fatalf("GPS = %+v, want %+v", got.GPS, want)
	}
}

func TestParserFallsBackToInternalWhenExternalIsOutsideVideoWindow(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(5, 10*testSecond),
		qRoadEvent(5, 11*testSecond),
		gpsEvent(9*testSecond, true, 22.5431, 114.0579, 0, 0, 1),
		gpsEvent(10*testSecond+250_000_000, false, 23.1291, 113.2644, 0, 0, 1),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GPS) != 1 || got.GPS[0].Latitude != 23.1291 || got.GPS[0].Longitude != 113.2644 {
		t.Fatalf("GPS = %+v, want internal Guangzhou sample", got.GPS)
	}
}

func TestParserAllInvalidGPSKeepsOtherTelemetryAndReturnsEmptySlice(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(6, 10*testSecond),
		carStateEvent(10*testSecond, 12.5),
		selfdriveStateEvent(10*testSecond, cereal.SelfdriveState_OpenpilotState_enabled, true, true, "", ""),
		gpsEventWithFix(10*testSecond, true, false, 0, 0, 0, 0, 4_294_967.5),
		gpsEvent(10*testSecond, false, 0, 0, 0, 0, 1),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 6)
	if err != nil {
		t.Fatal(err)
	}
	if got.GPS == nil || len(got.GPS) != 0 {
		t.Fatalf("GPS = %#v, want non-nil empty slice", got.GPS)
	}
	if len(got.Speeds) != 1 || len(got.Controls) != 1 {
		t.Fatalf("non-GPS telemetry changed: %+v", got)
	}
}

func TestParserGuangzhouGPSIsPresentInJSON(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(9, 10*testSecond),
		gpsEvent(10*testSecond, false, 23.1291, 113.2644, 0, 0, 1),
	)
	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 9)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"lat":23.1291`)) ||
		!bytes.Contains(data, []byte(`"lon":113.2644`)) {
		t.Fatalf("JSON = %s, want Guangzhou coordinates", data)
	}
}

func TestParserQRoadDuration(t *testing.T) {
	tests := []struct {
		name       string
		timestamps []uint64
		wantStart  uint64
		want       float64
		estimated  bool
	}{
		{
			name:       "even number of deltas with order and duplicate handling",
			timestamps: []uint64{4 * testSecond, testSecond, 2 * testSecond, 2 * testSecond},
			wantStart:  testSecond,
			want:       4.5,
		},
		{
			name:       "odd number of deltas",
			timestamps: []uint64{7 * testSecond, testSecond, 4 * testSecond, 2 * testSecond},
			wantStart:  testSecond,
			want:       8,
		},
		{
			name:       "one frame",
			timestamps: []uint64{9 * testSecond},
			wantStart:  9 * testSecond,
			want:       EstimatedSegmentDuration,
			estimated:  true,
		},
		{
			name:      "no frame",
			want:      EstimatedSegmentDuration,
			estimated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builders := make([]eventBuilder, 0, len(tt.timestamps))
			for _, timestamp := range tt.timestamps {
				builders = append(builders, qRoadEvent(3, timestamp))
			}
			got, err := NewParser().ParseSegment(bytes.NewReader(compressedEvents(t, builders...)), 3)
			if err != nil {
				t.Fatal(err)
			}
			if got.VideoStartMonoTime != tt.wantStart || got.Duration != tt.want || got.DurationEstimated != tt.estimated {
				t.Fatalf("timing = (%d, %v, %v), want (%d, %v, %v)",
					got.VideoStartMonoTime, got.Duration, got.DurationEstimated,
					tt.wantStart, tt.want, tt.estimated)
			}
			if got.Speeds == nil || got.GPS == nil || got.Controls == nil {
				t.Fatalf("successful slices must be initialized: %+v", got)
			}
		})
	}
}

func TestParserWindowFilteringAndStableOrdering(t *testing.T) {
	stream := compressedEvents(t,
		carStateEvent(11*testSecond, 1),
		carStateEvent(9*testSecond, 9),
		qRoadEvent(4, 12*testSecond),
		carStateEvent(10*testSecond, 0),
		carStateEvent(11*testSecond, 2),
		carStateEvent(14*testSecond, 4),
		carStateEvent(14*testSecond+1, 5),
		qRoadEvent(4, 10*testSecond),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []SpeedSample{
		{Time: 0, Value: 0},
		{Time: 1, Value: 1},
		{Time: 1, Value: 2},
		{Time: 4, Value: 4},
	}
	if !reflect.DeepEqual(got.Speeds, want) {
		t.Fatalf("Speeds = %+v, want %+v", got.Speeds, want)
	}
}

func TestParserUnknownSelfdriveState(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(1, testSecond),
		selfdriveStateEvent(testSecond, cereal.SelfdriveState_OpenpilotState(99), false, false, "", ""),
	)

	got, err := NewParser().ParseSegment(bytes.NewReader(stream), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Controls) != 1 || got.Controls[0].State != "unknown" {
		t.Fatalf("Controls = %+v", got.Controls)
	}
}

func TestParserEmptyZstdStream(t *testing.T) {
	got, err := NewParser().ParseSegment(bytes.NewReader(compressBytes(t, nil)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Segment != 0 || got.VideoStartMonoTime != 0 ||
		got.Duration != EstimatedSegmentDuration || !got.DurationEstimated {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.Speeds == nil || got.GPS == nil || got.Controls == nil {
		t.Fatalf("successful slices must be initialized: %+v", got)
	}
}

func TestParserRejectsRawEmptyInput(t *testing.T) {
	_, err := NewParser().ParseSegment(bytes.NewReader(nil), 0)
	if !errors.Is(err, ErrZstd) {
		t.Fatalf("error = %v, want ErrZstd", err)
	}
}

func TestParserRejectsNilReader(t *testing.T) {
	_, err := NewParser().ParseSegment(nil, 0)
	if !errors.Is(err, ErrZstd) {
		t.Fatalf("error = %v, want ErrZstd", err)
	}
}

func TestParserRejectsNegativeSegment(t *testing.T) {
	_, err := NewParser().ParseSegment(bytes.NewReader(nil), -1)
	if !errors.Is(err, ErrInvalidSegment) {
		t.Fatalf("error = %v, want ErrInvalidSegment", err)
	}
}

func TestParserSegmentInt32Boundary(t *testing.T) {
	t.Run("max int32", func(t *testing.T) {
		stream := compressedEvents(t, qRoadEvent(math.MaxInt32, testSecond))
		got, err := NewParser().ParseSegment(bytes.NewReader(stream), math.MaxInt32)
		if err != nil {
			t.Fatal(err)
		}
		if got.VideoStartMonoTime != testSecond {
			t.Fatalf("telemetry = %+v", got)
		}
	})

	t.Run("uint32 wrap to zero", func(t *testing.T) {
		const segment = int(4_294_967_296)
		stream := compressedEvents(t, qRoadEvent(0, testSecond))
		got, err := NewParser().ParseSegment(bytes.NewReader(stream), segment)
		if !errors.Is(err, ErrInvalidSegment) {
			t.Fatalf("telemetry/error = %+v/%v, want ErrInvalidSegment", got, err)
		}
	})
}

func TestParserRejectsNegativeLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Parser)
	}{
		{
			name: "decompressed bytes",
			mutate: func(parser *Parser) {
				parser.MaxDecompressedBytes = -1
			},
		},
		{
			name: "events",
			mutate: func(parser *Parser) {
				parser.MaxEvents = -1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			tt.mutate(&parser)
			_, err := parser.ParseSegment(bytes.NewReader(nil), 0)
			if !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("error = %v, want ErrInvalidLimits", err)
			}
		})
	}
}

func TestParserHidesBytesAndLenFastPath(t *testing.T) {
	stream := compressedEvents(t, qRoadEvent(0, testSecond))
	reader := &panicByter{Reader: bytes.NewReader(stream)}

	if _, err := NewParser().ParseSegment(reader, 0); err != nil {
		t.Fatal(err)
	}
}

func TestParserRejectsOversizedZstdWindow(t *testing.T) {
	plain := bytes.Repeat([]byte{0}, 2*zstd.MinWindowSize)
	stream := compressBytesWithOptions(t, plain, zstd.WithWindowSize(1<<20))
	parser := NewParser()
	parser.MaxDecompressedBytes = zstd.MinWindowSize

	_, err := parser.ParseSegment(bytes.NewReader(stream), 0)
	if !errors.Is(err, ErrZstd) {
		t.Fatalf("error = %v, want ErrZstd", err)
	}
}

func TestParserClassifiesZstdErrors(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		_, err := NewParser().ParseSegment(bytes.NewReader([]byte("not zstd")), 0)
		if !errors.Is(err, ErrZstd) {
			t.Fatalf("error = %v, want ErrZstd", err)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		stream := compressedEvents(t, qRoadEvent(0, testSecond))
		stream = stream[:len(stream)-1]
		_, err := NewParser().ParseSegment(bytes.NewReader(stream), 0)
		if !errors.Is(err, ErrZstd) {
			t.Fatalf("error = %v, want ErrZstd", err)
		}
	})
}

func TestParserClassifiesCapnpErrors(t *testing.T) {
	_, err := NewParser().ParseSegment(bytes.NewReader(compressBytes(t, []byte{0, 0, 0, 0})), 0)
	if !errors.Is(err, ErrCapnp) {
		t.Fatalf("error = %v, want ErrCapnp", err)
	}
}

func TestParserDecompressedLimit(t *testing.T) {
	plain := eventBytes(t, qRoadEvent(0, testSecond))
	stream := compressBytes(t, plain)

	t.Run("exact boundary", func(t *testing.T) {
		parser := NewParser()
		parser.MaxDecompressedBytes = int64(len(plain))
		if _, err := parser.ParseSegment(bytes.NewReader(stream), 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("one byte beyond", func(t *testing.T) {
		parser := NewParser()
		parser.MaxDecompressedBytes = int64(len(plain) - 1)
		_, err := parser.ParseSegment(bytes.NewReader(stream), 0)
		if !errors.Is(err, ErrDecompressedLimit) {
			t.Fatalf("error = %v, want ErrDecompressedLimit", err)
		}
	})
}

func TestParserEventLimit(t *testing.T) {
	stream := compressedEvents(t,
		qRoadEvent(0, testSecond),
		unknownEvent(testSecond),
	)

	t.Run("exactly at limit", func(t *testing.T) {
		parser := NewParser()
		parser.MaxEvents = 2
		if _, err := parser.ParseSegment(bytes.NewReader(stream), 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("one over limit", func(t *testing.T) {
		parser := NewParser()
		parser.MaxEvents = 1
		_, err := parser.ParseSegment(bytes.NewReader(stream), 0)
		if !errors.Is(err, ErrEventLimit) {
			t.Fatalf("error = %v, want ErrEventLimit", err)
		}
	})
}

func TestParserMessageSizeLimit(t *testing.T) {
	parser := NewParser()
	parser.MaxMessageBytes = 8
	_, err := parser.ParseSegment(bytes.NewReader(compressedEvents(t, qRoadEvent(0, testSecond))), 0)
	if !errors.Is(err, ErrCapnp) {
		t.Fatalf("error = %v, want ErrCapnp", err)
	}
}

func TestParserReaderErrorDoesNotPanic(t *testing.T) {
	sourceErr := errors.New("source read failed")
	stream := compressedEvents(t, qRoadEvent(0, testSecond))
	reader := io.MultiReader(bytes.NewReader(stream[:1]), errorReader{err: sourceErr})

	_, err := NewParser().ParseSegment(reader, 0)
	if !errors.Is(err, ErrZstd) || !errors.Is(err, sourceErr) {
		t.Fatalf("error = %v, want ErrZstd wrapping source error", err)
	}
}

func TestParserNonEventRootDoesNotPanic(t *testing.T) {
	message, segment := capnp.NewSingleSegmentMessage(nil)
	if _, err := capnp.NewRootStruct(segment, capnp.ObjectSize{}); err != nil {
		t.Fatal(err)
	}
	plain, err := message.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	got, err := NewParser().ParseSegment(bytes.NewReader(compressBytes(t, plain)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Speeds) != 0 || len(got.GPS) != 0 || len(got.Controls) != 0 {
		t.Fatalf("unexpected telemetry: %+v", got)
	}
}

func TestSegmentTelemetryJSONTags(t *testing.T) {
	telemetryType := reflect.TypeOf(SegmentTelemetry{})
	want := map[string]string{
		"Segment": "segment", "Duration": "duration", "DurationEstimated": "duration_estimated",
		"VideoStartMonoTime": "video_start_mono_time", "Speeds": "speeds", "GPS": "gps", "Controls": "controls",
	}
	for fieldName, tag := range want {
		field, ok := telemetryType.FieldByName(fieldName)
		if !ok || field.Tag.Get("json") != tag {
			t.Errorf("%s json tag = %q, want %q", fieldName, field.Tag.Get("json"), tag)
		}
	}
}

type eventBuilder func(cereal.Event) error

func qRoadEvent(segment int32, timestamp uint64) eventBuilder {
	return func(event cereal.Event) error {
		index, err := event.NewQRoadEncodeIdx()
		if err != nil {
			return err
		}
		index.SetSegmentNum(segment)
		index.SetTimestampSof(timestamp)
		return nil
	}
}

func carStateEvent(timestamp uint64, speed float32) eventBuilder {
	return func(event cereal.Event) error {
		event.SetLogMonoTime(timestamp)
		state, err := event.NewCarState()
		if err != nil {
			return err
		}
		state.SetVEgo(speed)
		return nil
	}
}

func selfdriveStateEvent(timestamp uint64, state cereal.SelfdriveState_OpenpilotState, enabled, active bool, alert1, alert2 string) eventBuilder {
	return func(event cereal.Event) error {
		event.SetLogMonoTime(timestamp)
		value, err := event.NewSelfdriveState()
		if err != nil {
			return err
		}
		value.SetState(state)
		value.SetEnabled(enabled)
		value.SetActive(active)
		if err := value.SetAlertText1(alert1); err != nil {
			return err
		}
		return value.SetAlertText2(alert2)
	}
}

func gpsEvent(timestamp uint64, external bool, lat, lon float64, speed, bearing, accuracy float32) eventBuilder {
	return gpsEventWithFix(timestamp, external, true, lat, lon, speed, bearing, accuracy)
}

func gpsEventWithFix(timestamp uint64, external, hasFix bool, lat, lon float64, speed, bearing, accuracy float32) eventBuilder {
	return func(event cereal.Event) error {
		event.SetLogMonoTime(timestamp)
		var (
			gps cereal.GpsLocationData
			err error
		)
		if external {
			gps, err = event.NewGpsLocationExternal()
		} else {
			gps, err = event.NewGpsLocation()
		}
		if err != nil {
			return err
		}
		gps.SetHasFix(hasFix)
		gps.SetLatitude(lat)
		gps.SetLongitude(lon)
		gps.SetSpeed(speed)
		gps.SetBearingDeg(bearing)
		gps.SetHorizontalAccuracy(accuracy)
		return nil
	}
}

func unknownEvent(timestamp uint64) eventBuilder {
	return func(event cereal.Event) error {
		event.SetLogMonoTime(timestamp)
		_, err := event.NewBoot()
		return err
	}
}

func compressedEvents(t *testing.T, builders ...eventBuilder) []byte {
	t.Helper()
	return compressBytes(t, eventBytes(t, builders...))
}

func eventBytes(t *testing.T, builders ...eventBuilder) []byte {
	t.Helper()
	var plain bytes.Buffer
	for _, build := range builders {
		message, segment := capnp.NewSingleSegmentMessage(nil)
		event, err := cereal.NewRootEvent(segment)
		if err != nil {
			t.Fatal(err)
		}
		if err := build(event); err != nil {
			t.Fatal(err)
		}
		data, err := message.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		plain.Write(data)
	}
	return plain.Bytes()
}

func compressBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	return compressBytesWithOptions(t, plain, zstd.WithWindowSize(zstd.MinWindowSize))
}

func compressBytesWithOptions(t *testing.T, plain []byte, options ...zstd.EOption) []byte {
	t.Helper()
	var compressed bytes.Buffer
	options = append(options, zstd.WithEncoderConcurrency(1))
	encoder, err := zstd.NewWriter(&compressed, options...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type panicByter struct {
	*bytes.Reader
}

func (*panicByter) Bytes() []byte {
	panic("zstd Bytes fast path used")
}
