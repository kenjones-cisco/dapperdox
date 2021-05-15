package models

import "testing"

func TestEvent_String(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "EventAdd",
			event: EventAdd,
			want:  "add",
		},
		{
			name:  "EventUpdate",
			event: EventUpdate,
			want:  "update",
		},
		{
			name:  "EventDelete",
			event: EventDelete,
			want:  "delete",
		},
		{
			name:  "unknown event",
			event: Event(999999999),
			want:  "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.String(); got != tt.want {
				t.Errorf("Event.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
