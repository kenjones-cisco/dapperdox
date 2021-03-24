package models

// Event represents a registry update event.
type Event int

const (
	// EventAdd is sent when an object is added.
	EventAdd Event = iota

	// EventUpdate is sent when an object is modified
	// Captures the modified object.
	EventUpdate

	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state.
	EventDelete
)

func (e Event) String() string {
	out := "unknown"

	switch e {
	case EventAdd:
		out = "add"
	case EventUpdate:
		out = "update"
	case EventDelete:
		out = "delete"
	}

	return out
}
