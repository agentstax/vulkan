package consumergroup

import "time"

// DeclarationOutcome is where one DeclareBindings attempt ended up.
type DeclarationOutcome string

const (
	DeclarationInstalled DeclarationOutcome = "installed" // the declared set is now the group's effective set
	DeclarationJoined    DeclarationOutcome = "joined"    // the declared set was already stored
	DeclarationWaiting   DeclarationOutcome = "waiting"   // a live instance still declares a different stored set
)

// Declaration is one declarer's newest declaration on a group.
// DeclarationInstalled is the group's effective set.
// DeclarationWaiting a declarer still blocked on changing effective set.
type Declaration struct {
	GroupName     string             `json:"group"`
	TopicName     string             `json:"topic"`
	SchemaVersion int64              `json:"version"`
	Status        DeclarationOutcome `json:"status"`
	Patterns      []string           `json:"patterns"` // empty = the whole topic
	DeclaredBy    string             `json:"declared_by"`
	DeclaredAt    time.Time          `json:"declared_at"`
	AttemptAt     time.Time          `json:"attempt_at"`
}
