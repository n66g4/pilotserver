package replay

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"capnproto.org/go/capnp/v3"
	"github.com/klauspost/compress/zstd"

	"pilotserver/internal/replay/cereal"
)

const (
	DefaultMaxDecompressedBytes int64  = 256 << 20
	DefaultMaxEvents                   = 2_000_000
	DefaultMaxMessageBytes      uint64 = 64 << 20
	EstimatedSegmentDuration           = 60.0
)

var (
	ErrInvalidSegment    = errors.New("invalid segment")
	ErrZstd              = errors.New("invalid zstd stream")
	ErrCapnp             = errors.New("invalid capnp stream")
	ErrDecompressedLimit = errors.New("decompressed limit exceeded")
	ErrEventLimit        = errors.New("event limit exceeded")
	ErrInvalidLimits     = errors.New("invalid parser limits")
)

type Parser struct {
	MaxDecompressedBytes int64
	MaxEvents            int
	MaxMessageBytes      uint64
}

func NewParser() Parser {
	return Parser{
		MaxDecompressedBytes: DefaultMaxDecompressedBytes,
		MaxEvents:            DefaultMaxEvents,
		MaxMessageBytes:      DefaultMaxMessageBytes,
	}
}

func (p Parser) ParseSegment(r io.Reader, segment int) (SegmentTelemetry, error) {
	result := emptyTelemetry(segment)
	if segment < 0 || segment > math.MaxInt32 {
		return result, fmt.Errorf("%w: %d is outside int32 range", ErrInvalidSegment, segment)
	}
	if r == nil {
		return result, fmt.Errorf("%w: nil input reader", ErrZstd)
	}

	maxBytes, maxEvents, maxMessageBytes, err := p.limits()
	if err != nil {
		return result, err
	}
	decoderMemory := uint64(maxBytes)
	if decoderMemory < zstd.MinWindowSize {
		decoderMemory = zstd.MinWindowSize
	}
	compressed := &compressedReader{reader: r}
	zstdReader, err := zstd.NewReader(
		compressed,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxMemory(decoderMemory),
		zstd.WithDecoderMaxWindow(decoderMemory),
	)
	if err != nil {
		return result, fmt.Errorf("%w: initialize decoder: %w", ErrZstd, err)
	}
	defer zstdReader.Close()

	stream := &decompressedLimitReader{reader: zstdReader, remaining: maxBytes}
	decoder := capnp.NewDecoder(stream)
	decoder.MaxMessageSize = maxMessageBytes
	var collected rawTelemetry

	for events := 0; ; events++ {
		message, err := decoder.Decode()
		if errors.Is(err, io.EOF) {
			if compressed.bytesRead == 0 {
				return result, fmt.Errorf("%w: empty input", ErrZstd)
			}
			break
		}
		if err != nil {
			return result, classifyDecodeError(err)
		}
		if events == maxEvents {
			return result, fmt.Errorf("%w: maximum %d", ErrEventLimit, maxEvents)
		}

		event, err := cereal.ReadRootEvent(message)
		if err != nil {
			return result, fmt.Errorf("%w: read event root: %w", ErrCapnp, err)
		}
		if !event.IsValid() {
			continue
		}
		if err := collected.addEvent(event, segment); err != nil {
			return result, fmt.Errorf("%w: extract event: %w", ErrCapnp, err)
		}
	}

	return collected.normalize(result), nil
}

func (p Parser) limits() (int64, int, uint64, error) {
	maxBytes := p.MaxDecompressedBytes
	if maxBytes < 0 {
		return 0, 0, 0, fmt.Errorf("%w: MaxDecompressedBytes must not be negative", ErrInvalidLimits)
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxDecompressedBytes
	}
	maxEvents := p.MaxEvents
	if maxEvents < 0 {
		return 0, 0, 0, fmt.Errorf("%w: MaxEvents must not be negative", ErrInvalidLimits)
	}
	if maxEvents == 0 {
		maxEvents = DefaultMaxEvents
	}
	maxMessageBytes := p.MaxMessageBytes
	if maxMessageBytes == 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	return maxBytes, maxEvents, maxMessageBytes, nil
}

func emptyTelemetry(segment int) SegmentTelemetry {
	return SegmentTelemetry{
		Segment:           segment,
		Duration:          EstimatedSegmentDuration,
		DurationEstimated: true,
		Speeds:            make([]SpeedSample, 0),
		GPS:               make([]GPSSample, 0),
		Controls:          make([]ControlSample, 0),
	}
}

func classifyDecodeError(err error) error {
	if errors.Is(err, ErrDecompressedLimit) {
		return fmt.Errorf("%w: %w", ErrDecompressedLimit, err)
	}
	var streamErr *zstdStreamError
	if errors.As(err, &streamErr) {
		return fmt.Errorf("%w: decode: %w", ErrZstd, err)
	}
	return fmt.Errorf("%w: decode: %w", ErrCapnp, err)
}

type zstdStreamError struct {
	err error
}

func (e *zstdStreamError) Error() string {
	return e.err.Error()
}

func (e *zstdStreamError) Unwrap() error {
	return e.err
}

type compressedReader struct {
	reader    io.Reader
	bytesRead uint64
}

func (r *compressedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += uint64(n)
	return n, err
}

type decompressedLimitReader struct {
	reader    io.Reader
	remaining int64
	pending   error
}

func (r *decompressedLimitReader) Read(p []byte) (int, error) {
	if r.pending != nil {
		err := r.pending
		r.pending = nil
		return 0, err
	}
	if r.remaining == 0 {
		var one [1]byte
		n, err := r.reader.Read(one[:])
		if n > 0 {
			return 0, ErrDecompressedLimit
		}
		return 0, wrapZstdReadError(err)
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}

	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if err == nil || n == 0 {
		return n, wrapZstdReadError(err)
	}
	r.pending = wrapZstdReadError(err)
	return n, nil
}

func wrapZstdReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, ErrDecompressedLimit) {
		return err
	}
	return &zstdStreamError{err: err}
}

type timestampedSpeed struct {
	timestamp uint64
	value     float32
}

type timestampedGPS struct {
	timestamp uint64
	sample    GPSSample
}

type timestampedControl struct {
	timestamp uint64
	sample    ControlSample
}

type rawTelemetry struct {
	speeds      []timestampedSpeed
	internalGPS []timestampedGPS
	externalGPS []timestampedGPS
	controls    []timestampedControl
	qRoad       []uint64
}

func (r *rawTelemetry) addEvent(event cereal.Event, segment int) error {
	switch event.Which() {
	case cereal.Event_Which_carState:
		state, err := event.CarState()
		if err != nil {
			return err
		}
		if !state.IsValid() {
			return errors.New("carState is missing")
		}
		r.speeds = append(r.speeds, timestampedSpeed{event.LogMonoTime(), state.VEgo()})

	case cereal.Event_Which_selfdriveState:
		state, err := event.SelfdriveState()
		if err != nil {
			return err
		}
		if !state.IsValid() {
			return errors.New("selfdriveState is missing")
		}
		alert1, err := state.AlertText1()
		if err != nil {
			return err
		}
		alert2, err := state.AlertText2()
		if err != nil {
			return err
		}
		stateName := state.State().String()
		if stateName == "" {
			stateName = "unknown"
		}
		r.controls = append(r.controls, timestampedControl{
			timestamp: event.LogMonoTime(),
			sample: ControlSample{
				Enabled: state.Enabled(), Active: state.Active(), State: stateName,
				AlertText1: alert1, AlertText2: alert2,
			},
		})

	case cereal.Event_Which_gpsLocationExternal:
		gps, err := event.GpsLocationExternal()
		if err != nil {
			return err
		}
		if !gps.IsValid() {
			return errors.New("gpsLocationExternal is missing")
		}
		if sample, ok := makeGPS(event.LogMonoTime(), gps); ok {
			r.externalGPS = append(r.externalGPS, sample)
		}

	case cereal.Event_Which_gpsLocation:
		gps, err := event.GpsLocation()
		if err != nil {
			return err
		}
		if !gps.IsValid() {
			return errors.New("gpsLocation is missing")
		}
		if sample, ok := makeGPS(event.LogMonoTime(), gps); ok {
			r.internalGPS = append(r.internalGPS, sample)
		}

	case cereal.Event_Which_qRoadEncodeIdx:
		index, err := event.QRoadEncodeIdx()
		if err != nil {
			return err
		}
		if !index.IsValid() {
			return errors.New("qRoadEncodeIdx is missing")
		}
		if index.SegmentNum() == int32(segment) {
			r.qRoad = append(r.qRoad, index.TimestampSof())
		}
	}
	return nil
}

func makeGPS(timestamp uint64, gps cereal.GpsLocationData) (timestampedGPS, bool) {
	latitude, longitude := gps.Latitude(), gps.Longitude()
	if !gps.HasFix() ||
		math.IsNaN(latitude) || math.IsInf(latitude, 0) ||
		math.IsNaN(longitude) || math.IsInf(longitude, 0) ||
		latitude < -90 || latitude > 90 ||
		longitude < -180 || longitude > 180 ||
		(latitude == 0 && longitude == 0) {
		return timestampedGPS{}, false
	}
	return timestampedGPS{
		timestamp: timestamp,
		sample: GPSSample{
			Latitude: latitude, Longitude: longitude, Speed: gps.Speed(),
			BearingDeg: gps.BearingDeg(), HorizontalAccuracy: gps.HorizontalAccuracy(),
		},
	}, true
}

func (r *rawTelemetry) normalize(result SegmentTelemetry) SegmentTelemetry {
	start, duration, estimated := videoTiming(r.qRoad)
	result.VideoStartMonoTime = start
	result.Duration = duration
	result.DurationEstimated = estimated
	if len(r.qRoad) == 0 {
		return result
	}

	for _, sample := range r.speeds {
		if relative, ok := relativeTime(sample.timestamp, start, duration); ok {
			result.Speeds = append(result.Speeds, SpeedSample{Time: relative, Value: sample.value})
		}
	}
	for _, sample := range r.controls {
		if relative, ok := relativeTime(sample.timestamp, start, duration); ok {
			sample.sample.Time = relative
			result.Controls = append(result.Controls, sample.sample)
		}
	}

	external := normalizeGPS(r.externalGPS, start, duration)
	if len(external) > 0 {
		result.GPS = external
	} else {
		result.GPS = normalizeGPS(r.internalGPS, start, duration)
	}

	sort.SliceStable(result.Speeds, func(i, j int) bool { return result.Speeds[i].Time < result.Speeds[j].Time })
	sort.SliceStable(result.GPS, func(i, j int) bool { return result.GPS[i].Time < result.GPS[j].Time })
	sort.SliceStable(result.Controls, func(i, j int) bool { return result.Controls[i].Time < result.Controls[j].Time })
	return result
}

func normalizeGPS(samples []timestampedGPS, start uint64, duration float64) []GPSSample {
	result := make([]GPSSample, 0, len(samples))
	for _, sample := range samples {
		if relative, ok := relativeTime(sample.timestamp, start, duration); ok {
			sample.sample.Time = relative
			result = append(result, sample.sample)
		}
	}
	return result
}

func relativeTime(timestamp, start uint64, duration float64) (float64, bool) {
	if timestamp < start {
		return 0, false
	}
	relative := float64(timestamp-start) / float64(nanosecondsPerSecond)
	return relative, relative <= duration
}

const nanosecondsPerSecond = uint64(1_000_000_000)

func videoTiming(timestamps []uint64) (uint64, float64, bool) {
	if len(timestamps) == 0 {
		return 0, EstimatedSegmentDuration, true
	}

	sorted := append([]uint64(nil), timestamps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	unique := sorted[:1]
	for _, timestamp := range sorted[1:] {
		if timestamp != unique[len(unique)-1] {
			unique = append(unique, timestamp)
		}
	}
	if len(unique) == 1 {
		return unique[0], EstimatedSegmentDuration, true
	}

	deltas := make([]uint64, len(unique)-1)
	for i := 1; i < len(unique); i++ {
		deltas[i-1] = unique[i] - unique[i-1]
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
	middle := len(deltas) / 2
	median := float64(deltas[middle])
	if len(deltas)%2 == 0 {
		median = (float64(deltas[middle-1]) + median) / 2
	}
	duration := (float64(unique[len(unique)-1]-unique[0]) + median) / float64(nanosecondsPerSecond)
	return unique[0], duration, false
}
