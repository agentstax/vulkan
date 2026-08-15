package common

type updateState int

const (
	updateUnchanged updateState = iota
	updateSet
	updateUnset
)

// Update is one config field's requested change: the zero value leaves the
// field unchanged, Set writes a value, Unset returns the field to its
// default.
type Update[T any] struct {
	value T
	state updateState
}

// Set builds an Update that writes value to the field.
func Set[T any](value T) Update[T] {
	return Update[T]{value: value, state: updateSet}
}

// Unset builds an Update that returns the field to its default.
func Unset[T any]() Update[T] {
	return Update[T]{state: updateUnset}
}

// Value returns the value a Set carries; ok is false for the zero value and
// for Unset.
func (u Update[T]) Value() (T, bool) {
	return u.value, u.state == updateSet
}

// IsUnset reports whether the update returns the field to its default.
func (u Update[T]) IsUnset() bool {
	return u.state == updateUnset
}

// IsChanged reports whether the update does anything -- the zero value
// doesn't.
func (u Update[T]) IsChanged() bool {
	return u.state != updateUnchanged
}

// Any reshapes the update for a map of mixed value types.
func (u Update[T]) Any() Update[any] {
	return Update[any]{value: u.value, state: u.state}
}
