package common

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

// ProcessIdentity is hostname:pid:<random tiebreak>, computed once at startup
// and stable for the life of the process. Containers often run as pid 1 under
// cloned hostnames, so the random part is what keeps two such processes
// distinct.
var ProcessIdentity = func() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s:%d:%s", hostname, os.Getpid(), uuid.NewString()[:8])
}()
