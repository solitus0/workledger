package progress

type Mode string

const (
	ModeAuto  Mode = "auto"
	ModeBar   Mode = "bar"
	ModePlain Mode = "plain"
	ModeOff   Mode = "off"
)

type Event struct {
	Phase         string
	ScopeLabel    string
	PlannedAction string
	ScopeDone     int
	ScopeTotal    int
	WorkDone      int
	WorkTotal     int
	Active        int
	Failed        int
	Message       string
}
