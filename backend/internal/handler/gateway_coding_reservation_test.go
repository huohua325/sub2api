package handler

import (
	"context"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCodingUsageTaskSurvivesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	runCodingUsageTask(ctx, func(c context.Context) {
		require.NoError(t, c.Err())
		_, ok := c.Deadline()
		require.True(t, ok)
		called = true
	})
	require.True(t, called, "billing must run before return without submitting to a droppable worker")
}

func TestCodingReservationRealtimePartialAudioSurvivesUnexpectedClose(t *testing.T) {
	result, closeWithError := grokRealtimeCompletion("grok-voice-latest", time.Minute, true, coderws.CloseError{Code: coderws.StatusPolicyViolation})
	require.True(t, closeWithError)
	require.NotNil(t, result)
	require.Equal(t, float64(1), result.AudioUsage.DurationOrUnits)
	result, _ = grokRealtimeCompletion("grok-voice-latest", time.Minute, false, coderws.CloseError{Code: coderws.StatusPolicyViolation})
	require.Nil(t, result)
}
