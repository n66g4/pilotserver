package cereal

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"capnproto.org/go/capnp/v3"
)

func TestSchemaMetadata(t *testing.T) {
	if SourceCommit != "21d40d72c65021c81e84a62e23d700972c7c8a7f" {
		t.Fatalf("SourceCommit = %q", SourceCommit)
	}
	if SchemaVersion != "dragonpilot-21d40d7" {
		t.Fatalf("SchemaVersion = %q", SchemaVersion)
	}
}

func TestSchemaSnapshots(t *testing.T) {
	want := map[string]string{
		"log.capnp":         "3cfb10a1a1b44b810977cefbb55ad945b409909d9b9e6a2c0aade9a9c4adb8c3",
		"car.capnp":         "bc4b32367adea7428614b761eb1525f174a2c79612ecc51ab16395ec2e0af3bf",
		"custom.capnp":      "da8149adeeae6bafba017735d27910184ef4483579d693e732030a5762246775",
		"deprecated.capnp":  "2ff0763df1483fd3a7faad9d658fbcac0d5b90fe8e404f6bef9698e1013a1cba",
		"include/c++.capnp": "fb306076cd38c27af1aed20fac6395e9a46fbe5b5df6a248b5f1b6845a079c44",
	}

	for name, wantHash := range want {
		data, err := os.ReadFile(filepath.Join("schema", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != wantHash {
			t.Errorf("%s SHA-256 = %s, want %s", name, got, wantHash)
		}
	}
}

func TestRequiredEventBranchesRoundTrip(t *testing.T) {
	const monoTime = uint64(123456789)

	t.Run("gpsLocation", func(t *testing.T) {
		got := roundTripEvent(t, func(event Event) {
			gps, err := event.NewGpsLocation()
			if err != nil {
				t.Fatal(err)
			}
			setGPS(gps)
		})
		if got.LogMonoTime() != monoTime {
			t.Fatalf("LogMonoTime = %d", got.LogMonoTime())
		}
		assertWhich(t, got, Event_Which_gpsLocation)
		gps, err := got.GpsLocation()
		if err != nil {
			t.Fatal(err)
		}
		assertGPS(t, gps)
	})

	t.Run("gpsLocationExternal", func(t *testing.T) {
		got := roundTripEvent(t, func(event Event) {
			gps, err := event.NewGpsLocationExternal()
			if err != nil {
				t.Fatal(err)
			}
			setGPS(gps)
		})
		assertWhich(t, got, Event_Which_gpsLocationExternal)
		gps, err := got.GpsLocationExternal()
		if err != nil {
			t.Fatal(err)
		}
		assertGPS(t, gps)
	})

	t.Run("carState", func(t *testing.T) {
		got := roundTripEvent(t, func(event Event) {
			state, err := event.NewCarState()
			if err != nil {
				t.Fatal(err)
			}
			state.SetVEgo(12.5)
		})
		assertWhich(t, got, Event_Which_carState)
		state, err := got.CarState()
		if err != nil {
			t.Fatal(err)
		}
		if state.VEgo() != 12.5 {
			t.Fatalf("VEgo = %v", state.VEgo())
		}
	})

	t.Run("qRoadEncodeIdx", func(t *testing.T) {
		got := roundTripEvent(t, func(event Event) {
			index, err := event.NewQRoadEncodeIdx()
			if err != nil {
				t.Fatal(err)
			}
			index.SetSegmentNum(7)
			index.SetSegmentId(8)
			index.SetTimestampSof(9)
		})
		assertWhich(t, got, Event_Which_qRoadEncodeIdx)
		index, err := got.QRoadEncodeIdx()
		if err != nil {
			t.Fatal(err)
		}
		if index.SegmentNum() != 7 || index.SegmentId() != 8 || index.TimestampSof() != 9 {
			t.Fatalf("encode index = (%d, %d, %d)", index.SegmentNum(), index.SegmentId(), index.TimestampSof())
		}
	})

	t.Run("selfdriveState", func(t *testing.T) {
		got := roundTripEvent(t, func(event Event) {
			state, err := event.NewSelfdriveState()
			if err != nil {
				t.Fatal(err)
			}
			state.SetState(SelfdriveState_OpenpilotState_enabled)
			state.SetEnabled(true)
			state.SetActive(true)
			if err := state.SetAlertText1("alert one"); err != nil {
				t.Fatal(err)
			}
			if err := state.SetAlertText2("alert two"); err != nil {
				t.Fatal(err)
			}
		})
		assertWhich(t, got, Event_Which_selfdriveState)
		state, err := got.SelfdriveState()
		if err != nil {
			t.Fatal(err)
		}
		alert1, err := state.AlertText1()
		if err != nil {
			t.Fatal(err)
		}
		alert2, err := state.AlertText2()
		if err != nil {
			t.Fatal(err)
		}
		if state.State() != SelfdriveState_OpenpilotState_enabled ||
			!state.Enabled() || !state.Active() ||
			alert1 != "alert one" || alert2 != "alert two" {
			t.Fatalf("unexpected selfdrive state")
		}
	})
}

func TestOpenpilotStateEnumValues(t *testing.T) {
	values := []struct {
		value SelfdriveState_OpenpilotState
		name  string
	}{
		{SelfdriveState_OpenpilotState_disabled, "disabled"},
		{SelfdriveState_OpenpilotState_preEnabled, "preEnabled"},
		{SelfdriveState_OpenpilotState_enabled, "enabled"},
		{SelfdriveState_OpenpilotState_softDisabling, "softDisabling"},
		{SelfdriveState_OpenpilotState_overriding, "overriding"},
	}
	for i, value := range values {
		if int(value.value) != i {
			t.Errorf("%s = %d, want %d", value.name, value.value, i)
		}
		if got := value.value.String(); got != value.name {
			t.Errorf("%s.String() = %q", value.name, got)
		}
	}
}

func roundTripEvent(t *testing.T, populate func(Event)) Event {
	t.Helper()
	message, segment := capnp.NewSingleSegmentMessage(nil)
	event, err := NewRootEvent(segment)
	if err != nil {
		t.Fatal(err)
	}
	event.SetLogMonoTime(123456789)
	populate(event)

	data, err := message.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := capnp.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadRootEvent(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func setGPS(gps GpsLocationData) {
	gps.SetLatitude(37.5)
	gps.SetLongitude(-122.25)
	gps.SetSpeed(15.5)
	gps.SetBearingDeg(90.25)
	gps.SetHorizontalAccuracy(2.5)
}

func assertGPS(t *testing.T, gps GpsLocationData) {
	t.Helper()
	if gps.Latitude() != 37.5 ||
		gps.Longitude() != -122.25 ||
		gps.Speed() != 15.5 ||
		gps.BearingDeg() != 90.25 ||
		gps.HorizontalAccuracy() != 2.5 {
		t.Fatalf("unexpected GPS values")
	}
}

func assertWhich(t *testing.T, event Event, want Event_Which) {
	t.Helper()
	if got := event.Which(); got != want {
		t.Fatalf("Which() = %v, want %v", got, want)
	}
}
