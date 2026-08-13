package replay

type Segment struct {
	Number            int
	Duration          float64
	DurationEstimated bool
	QCameraRelPath    string
	QlogRelPath       string
	TelemetryError    string
}
