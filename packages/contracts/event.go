package contracts

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrEmptyEventID = errors.New("event id is required")

type EventEnvelope[T any] struct {
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	Version       int       `json:"version"`
	OccurredAt    time.Time `json:"occurred_at"`
	Producer      string    `json:"producer"`
	CorrelationID string    `json:"correlation_id"`
	CausationID   string    `json:"causation_id"`
	Payload       T         `json:"payload"`
}

func (e EventEnvelope[T]) Validate() error {
	if e.EventID == "" {
		return ErrEmptyEventID
	}
	return nil
}

func (e EventEnvelope[T]) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	type envelope EventEnvelope[T]
	return json.Marshal(envelope(e))
}

func (e *EventEnvelope[T]) UnmarshalJSON(data []byte) error {
	type envelope EventEnvelope[T]
	var decoded envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = EventEnvelope[T](decoded)
	return e.Validate()
}
