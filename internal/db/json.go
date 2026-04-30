package db

import (
	"encoding/json"
	"time"
)

// parseToEpochMs converts a timestamp string (RFC3339, RFC3339Nano, or SQLite format) to epoch milliseconds.
// Returns 0 if the string is empty or unparseable.
func parseToEpochMs(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func ptrEpochMs(s *string) *int64 {
	if s == nil || *s == "" {
		return nil
	}
	v := parseToEpochMs(*s)
	if v == 0 {
		return nil
	}
	return &v
}

// --- Custom JSON marshaling: timestamps as epoch ms ---

type callRecordAlias CallRecord

func (c CallRecord) MarshalJSON() ([]byte, error) {
	type aux struct {
		callRecordAlias
		StartTime  int64  `json:"start_time"`
		AnswerTime *int64 `json:"answer_time,omitempty"`
		EndTime    *int64 `json:"end_time,omitempty"`
		CreatedAt  int64  `json:"created_at"`
	}
	return json.Marshal(aux{
		callRecordAlias: callRecordAlias(c),
		StartTime:       parseToEpochMs(c.StartTime),
		AnswerTime:      ptrEpochMs(c.AnswerTime),
		EndTime:         ptrEpochMs(c.EndTime),
		CreatedAt:       parseToEpochMs(c.CreatedAt),
	})
}

type interactionLogAlias InteractionLog

func (l InteractionLog) MarshalJSON() ([]byte, error) {
	type aux struct {
		interactionLogAlias
		Timestamp int64 `json:"timestamp"`
	}
	return json.Marshal(aux{
		interactionLogAlias: interactionLogAlias(l),
		Timestamp:           parseToEpochMs(l.Timestamp),
	})
}

type systemLogAlias SystemLogRecord

func (l SystemLogRecord) MarshalJSON() ([]byte, error) {
	type aux struct {
		systemLogAlias
		Timestamp int64 `json:"timestamp"`
	}
	return json.Marshal(aux{
		systemLogAlias: systemLogAlias(l),
		Timestamp:      parseToEpochMs(l.Timestamp),
	})
}
