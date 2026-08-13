package replay

type SpeedSample struct {
	Time  float64 `json:"t"`
	Value float32 `json:"v"`
}

type GPSSample struct {
	Time               float64 `json:"t"`
	Latitude           float64 `json:"lat"`
	Longitude          float64 `json:"lon"`
	Speed              float32 `json:"speed"`
	BearingDeg         float32 `json:"bearing_deg"`
	HorizontalAccuracy float32 `json:"horizontal_accuracy"`
}

type ControlSample struct {
	Time       float64 `json:"t"`
	Enabled    bool    `json:"enabled"`
	Active     bool    `json:"active"`
	State      string  `json:"state"`
	AlertText1 string  `json:"alert_text_1,omitempty"`
	AlertText2 string  `json:"alert_text_2,omitempty"`
}

type SegmentTelemetry struct {
	Segment            int             `json:"segment"`
	Duration           float64         `json:"duration"`
	DurationEstimated  bool            `json:"duration_estimated"`
	VideoStartMonoTime uint64          `json:"video_start_mono_time"`
	Speeds             []SpeedSample   `json:"speeds"`
	GPS                []GPSSample     `json:"gps"`
	Controls           []ControlSample `json:"controls"`
}
