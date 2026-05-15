package progress

type Reporter interface {
	Start(Event)
	Event(Event)
	Finish(Event)
}

func NewNoopReporter() Reporter {
	return noopReporter{}
}

type noopReporter struct{}

func (noopReporter) Start(Event)  {}
func (noopReporter) Event(Event)  {}
func (noopReporter) Finish(Event) {}
