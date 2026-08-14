package binding

// DeclarationOutcome is where one DeclareBindings attempt ended up.
type DeclarationOutcome string

const (
	DeclarationInstalled DeclarationOutcome = "installed" // the declared set is now the group's effective set
	DeclarationJoined    DeclarationOutcome = "joined"    // the declared set was already stored
	DeclarationWaiting   DeclarationOutcome = "waiting"   // a live instance still declares a different stored set
)
