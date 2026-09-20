package main

import (
	"testing"

	"github.com/dimonomid/nerdlog/core"
	"github.com/stretchr/testify/assert"
)

// TestLogMsgFieldValueResolvesSyntheticLStreamMetadata verifies that a parsed
// field cannot replace Nerdlog's built-in lstream value.
func TestLogMsgFieldValueResolvesSyntheticLStreamMetadata(t *testing.T) {
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

// TestAddLogMsgFieldNamesIncludesSyntheticAndParsedFields verifies that select
// queries can see both built-in and parsed fields.
func TestAddLogMsgFieldNamesIncludesSyntheticAndParsedFields(t *testing.T) {
	names := newLogMsgFieldNamesSet()
	addLogMsgFieldNames(names, core.LogMsg{
		LogStreamName: "testhost",
		Context:       map[string]string{"program": "app"},
	})

	assert.Equal(t, map[string]struct{}{
		FieldNameTime:    {},
		FieldNameMessage: {},
		FieldNameLStream: {},
		"program":        {},
	}, names)
}
