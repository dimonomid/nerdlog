package main

import (
	"testing"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/cmd/nerdlog/ui"
	"github.com/dimonomid/nerdlog/core"
	"github.com/stretchr/testify/assert"
)

// TestLogMsgFieldValueResolvesBuiltInLStream verifies that a parsed
// field cannot replace Nerdlog's built-in lstream value.
func TestLogMsgFieldValueResolvesBuiltInLStream(t *testing.T) {
	msg := core.LogMsg{
		LogStreamName: "typed-lstream",
		Context: map[string]string{
			"lstream": "context-lstream",
		},
	}

	value, ok := logMsgFieldValue(msg, FieldNameLStream)

	assert.True(t, ok)
	assert.Equal(t, "typed-lstream", value)
}

// TestAddLogMsgFieldNamesIncludesBuiltInAndParsedFields verifies that select
// queries can see both built-in and parsed fields.
func TestAddLogMsgFieldNamesIncludesBuiltInAndParsedFields(t *testing.T) {
	names := newLogMsgFieldNamesSet()
	addLogMsgFieldNames(names, core.LogMsg{
		LogStreamName: "testhost",
		LogFilename:   "/var/log/messages",
		LogLinenumber: 123,
		Context:       map[string]string{"program": "app"},
	})

	assert.Equal(t, map[string]struct{}{
		FieldNameTime:      {},
		FieldNameMessage:   {},
		FieldNameLStream:   {},
		FieldNameLogFile:   {},
		FieldNameLogLineno: {},
		"program":          {},
	}, names)
}

// TestLogMsgFileFieldsResolveBuiltInValues verifies that parsed fields cannot
// replace the source file and line number stored on the message.
func TestLogMsgFileFieldsResolveBuiltInValues(t *testing.T) {
	msg := core.LogMsg{
		LogFilename:   "/var/log/messages",
		LogLinenumber: 123,
		Context: map[string]string{
			FieldNameLogFile:   "parsed-file",
			FieldNameLogLineno: "456",
		},
	}

	logfile, logfileExists := logMsgFieldValue(msg, FieldNameLogFile)
	loglineno, loglinenoExists := logMsgFieldValue(msg, FieldNameLogLineno)

	assert.True(t, logfileExists)
	assert.Equal(t, "/var/log/messages", logfile)
	assert.True(t, loglinenoExists)
	assert.Equal(t, "123", loglineno)
}

// TestJournalFieldsHideLogLineno verifies that journal records expose their
// source name without exposing the line number used internally for paging.
func TestJournalFieldsHideLogLineno(t *testing.T) {
	msg := core.LogMsg{
		LogFilename:   core.SpecialFilenameJournalctl,
		LogLinenumber: 123,
	}
	names := newLogMsgFieldNamesSet()
	addLogMsgFieldNames(names, msg)

	logfile, logfileExists := logMsgFieldValue(msg, FieldNameLogFile)
	loglineno, loglinenoExists := logMsgFieldValue(msg, FieldNameLogLineno)

	assert.True(t, logfileExists)
	assert.Equal(t, core.SpecialFilenameJournalctl, logfile)
	assert.False(t, loglinenoExists)
	assert.Empty(t, loglineno)
	assert.Contains(t, names, FieldNameLogFile)
	assert.NotContains(t, names, FieldNameLogLineno)
}

// TestBuiltInSourceFieldsCannotFilterRawLogs verifies that Row details does
// not offer a raw-text filter for values which are absent from raw log lines.
func TestBuiltInSourceFieldsCannotFilterRawLogs(t *testing.T) {
	assert.False(t, canFilterLogMsgFieldValue(FieldNameLStream))
	assert.False(t, canFilterLogMsgFieldValue(FieldNameLogFile))
	assert.False(t, canFilterLogMsgFieldValue(FieldNameLogLineno))
	assert.True(t, canFilterLogMsgFieldValue(FieldNameMessage))
	assert.True(t, canFilterLogMsgFieldValue("program"))
}

// TestWildcardIncludesBuiltInFileFields verifies that logfile and loglineno
// are added by "*" without being listed in the default select query.
func TestWildcardIncludesBuiltInFileFields(t *testing.T) {
	selectQuery, err := ParseSelectQuery(DefaultSelectQuery)
	assert.NoError(t, err)

	mv := MainView{
		params: MainViewParams{
			Clock:   clock.New(),
			Options: NewOptionsShared(Options{Timezone: time.UTC}),
		},
		selectQuery: selectQuery,
		logsTable:   ui.NewTable(),
	}

	fields := mv.updateTableHeader([]core.LogMsg{{
		LogStreamName: "testhost",
		LogFilename:   "/var/log/messages",
		LogLinenumber: 123,
		Context:       map[string]string{"hostname": "myhost"},
	}})

	assert.Equal(t, []string{
		FieldNameTime,
		FieldNameMessage,
		FieldNameLStream,
		"hostname",
		FieldNameLogFile,
		FieldNameLogLineno,
	}, fields)
}
