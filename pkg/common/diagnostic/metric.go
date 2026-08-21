package diagnostic

// Metric is a declared vulkan-owned metric: the name its measurements
// carry, plus the kind, unit, and description every rendering surface
// shares.
type Metric struct {
	Code        string
	Name        string
	Kind        string // "gauge" | "counter"
	Unit        string // UCUM code -- "" if none
	Description string
}

// NewMetric declares a metric and registers its code. The name must be
// unique too -- a measurement on the wire carries only its name, and
// GetMetric resolves the declaration by that handle.
func NewMetric(code string, name string, kind string, unit string, description string) *Metric {
	if name == "" {
		panic("name must not be empty: " + code)
	}
	if kind == "" {
		panic("kind must not be empty: " + code)
	}
	if description == "" {
		panic("description must not be empty: " + code)
	}
	if existing, ok := GetMetric(name); ok {
		panic("metric name already registered as " + existing.Code + ": " + name)
	}

	declared := &Metric{Code: code, Name: name, Kind: kind, Unit: unit, Description: description}
	register(declared)
	return declared
}

// Docs returns the metric's documentation page, derived from the code.
func (m *Metric) Docs() string {
	return docsBaseURL + m.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (m *Metric) GetCode() string {
	return m.Code
}

func (m *Metric) GetKind() Kind {
	return KindMetric
}

// Metrics lists every registered metric ordered by code.
func Metrics() []*Metric {
	return listRegistered[*Metric]()
}

// GetMetric returns the declaration behind a metric name; comma-ok absence
// for names the registry does not know (user metrics on the same topic).
// The registry fills once at init and stays small, so a scan serves.
func GetMetric(name string) (*Metric, bool) {
	registryLock.Lock()
	defer registryLock.Unlock()

	for _, registered := range registeredDeclarations {
		if declared, ok := registered.(*Metric); ok && declared.Name == name {
			return declared, true
		}
	}
	return nil, false
}
