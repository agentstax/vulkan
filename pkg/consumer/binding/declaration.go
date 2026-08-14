package binding

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
	GroupName     string
	TopicName     string
	SchemaVersion int64
	Status        DeclarationOutcome
	Patterns      []string // empty = the whole topic
	DeclaredBy    string
	DeclaredAt    time.Time
	AttemptAt     time.Time
}
