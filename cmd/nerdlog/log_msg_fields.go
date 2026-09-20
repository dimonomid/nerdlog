package main

import (
	"strconv"

	"github.com/dimonomid/nerdlog/core"
)

// newLogMsgFieldNamesSet returns the fields that exist even when no messages
// are loaded. The lstream field is added later only when a message has one, so
// an empty result still shows that field as unavailable in the table header.
func newLogMsgFieldNamesSet() map[string]struct{} {
	return map[string]struct{}{
		FieldNameTime:    {},
		FieldNameMessage: {},
	}
}

// addLogMsgFieldNames adds both parsed fields and Nerdlog's built-in fields to
// the names available in select queries. Journal records keep an internal line
// number for paging, but it is not a useful loglineno field and is not exposed.
func addLogMsgFieldNames(names map[string]struct{}, msg core.LogMsg) {
	if msg.LogStreamName != "" {
		names[FieldNameLStream] = struct{}{}
	}
	if msg.LogFilename != "" {
		names[FieldNameLogFile] = struct{}{}
	}
	if msg.LogFilename != "" && msg.LogFilename != core.SpecialFilenameJournalctl && msg.LogLinenumber > 0 {
		names[FieldNameLogLineno] = struct{}{}
	}
	for name := range msg.Context {
		names[name] = struct{}{}
	}
}

// logMsgFieldValue returns values for both parsed fields and built-in fields
// that are stored outside Context. Built-in values win if parsed fields use the
// same names. A journal record's internal paging number is not exposed.
func logMsgFieldValue(msg core.LogMsg, name string) (string, bool) {
	switch name {
	case FieldNameMessage:
		return msg.Msg, true
	case FieldNameLStream:
		return msg.LogStreamName, msg.LogStreamName != ""
	case FieldNameLogFile:
		return msg.LogFilename, msg.LogFilename != ""
	case FieldNameLogLineno:
		if msg.LogFilename == "" || msg.LogFilename == core.SpecialFilenameJournalctl || msg.LogLinenumber <= 0 {
			return "", false
		}
		return strconv.Itoa(msg.LogLinenumber), true
	default:
		value, ok := msg.Context[name]
		return value, ok
	}
}

// canFilterLogMsgFieldValue reports whether a field value comes from the raw
// log line searched by the remote awk query. Built-in source fields do not, so
// offering the Row details filter action for them would be misleading.
func canFilterLogMsgFieldValue(name string) bool {
	switch name {
	case FieldNameTime, FieldNameLStream, FieldNameLogFile, FieldNameLogLineno:
		return false
	default:
		return true
	}
}
