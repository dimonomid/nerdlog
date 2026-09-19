package main

import (
	"testing"

	"github.com/dimonomid/nerdlog/core"
	"github.com/juju/errors"
	"github.com/stretchr/testify/assert"
)

// TestQueryWarningSuppressionIsPerLogstream verifies that each logstream's
// visibility is independent and that any enabled stream triggers the dialog.
func TestQueryWarningSuppressionIsPerLogstream(t *testing.T) {
	app := &nerdlogApp{
		suppressedWarningLStreams: map[string]struct{}{},
	}
	warnings := []core.LogQueryWarning{
		{LStreamName: "one", Err: errors.New("first")},
		{LStreamName: "one", Err: errors.New("second")},
		{LStreamName: "two", Err: errors.New("third")},
	}

	app.setQueryWarningVisibility(map[string]bool{"one": false})

	assert.True(t, app.hasUnsuppressedQueryWarnings(warnings))
	assert.Equal(t, map[string]bool{"one": false, "two": true}, app.queryWarningVisibility(warnings))

	app.setQueryWarningVisibility(map[string]bool{"one": true, "two": false})
	assert.True(t, app.hasUnsuppressedQueryWarnings(warnings))
	assert.Equal(t, map[string]bool{"one": true, "two": false}, app.queryWarningVisibility(warnings))

	app.setQueryWarningVisibility(map[string]bool{"one": false, "two": false})
	assert.False(t, app.hasUnsuppressedQueryWarnings(warnings))
}
