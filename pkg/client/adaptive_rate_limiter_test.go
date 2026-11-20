package client

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveRateLimiter_ShouldWait(t *testing.T) {
	tests := []struct {
		desc           string
		remaining      int
		resetTime      time.Time
		safetyBuffer   int
		expectWait     bool
		expectWaitTime bool // true if we should get a positive wait time
	}{
		{
			desc:         "continues when plenty of requests remaining",
			remaining:    20,
			resetTime:    time.Now().Add(30 * time.Second),
			safetyBuffer: 15,
			expectWait:   false,
		},
		{
			desc:           "waits when exactly at safety buffer",
			remaining:      15,
			resetTime:      time.Now().Add(30 * time.Second),
			safetyBuffer:   15,
			expectWait:     true,
			expectWaitTime: true,
		},
		{
			desc:           "waits when below safety buffer",
			remaining:      10,
			resetTime:      time.Now().Add(30 * time.Second),
			safetyBuffer:   15,
			expectWait:     true,
			expectWaitTime: true,
		},
		{
			desc:         "resets state when reset time passed",
			remaining:    5,
			resetTime:    time.Now().Add(-30 * time.Second),
			safetyBuffer: 15,
			expectWait:   false,
		},
		{
			desc:         "handles zero requests with expired reset time",
			remaining:    0,
			resetTime:    time.Now().Add(-1 * time.Minute),
			safetyBuffer: 15,
			expectWait:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()

			rl := adaptiveRateLimiterTripper{}
			rl.remaining = tt.remaining
			rl.resetTime = tt.resetTime
			rl.safetyBuffer = tt.safetyBuffer

			shouldWait, waitTime := rl.shouldWait()

			assert.Equal(t, tt.expectWait, shouldWait)

			if tt.expectWaitTime {
				assert.Positive(t, waitTime)
			} else {
				assert.Equal(t, time.Duration(0), waitTime)
			}

			// If reset time was in the past, check that state was reset
			if tt.resetTime.Before(time.Now()) && !tt.expectWait {
				assert.Equal(t, 30, rl.remaining)
				assert.True(t, rl.resetTime.After(time.Now()))
			}
		})
	}
}

type statusTripper int

func (s statusTripper) RoundTrip(*http.Request) (*http.Response, error) {
	header := http.Header{
		http.CanonicalHeaderKey(headerRateRemaining): {"0"},
		http.CanonicalHeaderKey(headerRateReset):     {strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)},
	}

	return &http.Response{StatusCode: int(s), Header: header, Body: http.NoBody}, nil
}

func TestAdaptiveRateLimiterTripper_notModified(t *testing.T) {
	rl := &adaptiveRateLimiterTripper{safetyBuffer: 25, next: statusTripper(http.StatusNotModified)}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/o/r/tags", nil)
	require.NoError(t, err)

	// A 304 does not count in the rate limit: no wait, even with no request left.
	resp, err := rl.RoundTrip(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotModified, resp.StatusCode)

	// Any other response waits for the reset.
	rl.next = statusTripper(http.StatusOK)

	waited, err := rl.RoundTrip(req)
	if waited != nil {
		_ = waited.Body.Close()
	}

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
