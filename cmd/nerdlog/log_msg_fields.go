package main

import "github.com/dimonomid/nerdlog/core"

// newLogMsgFieldNamesSet returns the fields that exist even when no messages
// are loaded. The lstream field is added later only when a message has one, so
// an empty result still shows that field as unavailable in the table header.
func newLogMsgFieldNamesSet() map[string]struct{} {
	return map[string]struct{}{
		FieldNameTime:    {},
		FieldNameMessage: {},
	}
}

// addLogMsgFieldNames adds both parsed fields and Nerdlog's built-in lstream
// field to the names available in select queries.
func addLogMsgFieldNames(names map[string]struct{}, msg core.LogMsg) {
	if msg.LogStreamName != "" {
		names[FieldNameLStream] = struct{}{}
	}
	for name := range msg.Context {
		names[name] = struct{}{}
	}
}

// logMsgFieldValue returns values for both parsed fields and built-in fields
// that are stored outside Context. The built-in lstream value wins if a parsed
// field has the same name.
func logMsgFieldValue(msg core.LogMsg, name string) (string, bool) {
	switch name {
	case FieldNameMessage:
		return msg.Msg, true
	case FieldNameLStream:
		return msg.LogStreamName, msg.LogStreamName != ""
	default:
		value, ok := msg.Context[name]
		return value, ok
	}
}
