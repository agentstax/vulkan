package base

import "errors"

// ErrLeaseLost means the row was reclaimed by another consumer between the
// claim and the write.
var ErrLeaseLost = errors.New("lease lost: row reclaimed by another consumer")
