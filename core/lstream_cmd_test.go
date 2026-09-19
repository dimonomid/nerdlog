package core

import (
	"fmt"
	"testing"

	"github.com/juju/errors"
	"github.com/stretchr/testify/assert"
)

// TestLstreamCmdCtxQueryLogsWarningsAreBounded verifies that severe corruption
// retains the exact warning count while bounding details and summarizing those
// omitted from the response.
func TestLstreamCmdCtxQueryLogsWarningsAreBounded(t *testing.T) {
	ctx := &lstreamCmdCtxQueryLogs{Resp: &LogResp{}}

	const numWarnings = 10
	for i := 0; i < numWarnings; i++ {
		ctx.addWarning(errors.New(fmt.Sprintf("warning %d", i)))
	}
	ctx.finalizeWarnings()

	assert.Equal(t, numWarnings, ctx.Resp.NumWarnings)
	if assert.Len(t, ctx.Resp.Warnings, maxQueryWarnings+1) {
		assert.Equal(t, "warning 0", ctx.Resp.Warnings[0].Error())
		assert.Equal(t, "warning 4", ctx.Resp.Warnings[maxQueryWarnings-1].Error())
		assert.Equal(t, "skipped 5 additional malformed log records", ctx.Resp.Warnings[maxQueryWarnings].Error())
	}
}
