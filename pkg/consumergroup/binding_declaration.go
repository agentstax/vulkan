package consumergroup

import "time"

// BindingOutcome is where one DeclareBindings attempt ended up.
type BindingOutcome string

const (
	BindingInstalled BindingOutcome = "installed" // the declared set is now the group's effective set
	BindingJoined    BindingOutcome = "joined"    // the declared set was already stored
	BindingWaiting   BindingOutcome = "waiting"   // a live instance still declares a different stored set
)

// BindingDeclaration is one declarer's newest declaration on a group.
// BindingInstalled is the group's effective set.
// BindingWaiting a declarer still blocked on changing effective set.
type BindingDeclaration struct {
	GroupName   string         `json:"group"`
	TopicName   string         `json:"topic"`
	Status      BindingOutcome `json:"status"`
	Patterns    []string       `json:"patterns"` // empty = the whole topic
	DeclaredBy  string         `json:"declared_by"`
	DeclaredAt  time.Time      `json:"declared_at"`
	AttemptedAt time.Time      `json:"attempted_at"`
}
