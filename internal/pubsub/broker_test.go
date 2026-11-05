package pubsub

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Not parallel: it counts the goroutines of the whole test binary.
func TestShutdownStopsSubscriberGoroutines(t *testing.T) {
	const subscribers = 100

	before := runtime.NumGoroutine()

	b := NewBroker[int]()
	for range subscribers {
		b.Subscribe(context.Background())
	}
	require.GreaterOrEqual(t, runtime.NumGoroutine(), before+subscribers)

	b.Shutdown()

	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() < before+subscribers/2
	}, 2*time.Second, 10*time.Millisecond, "subscriber goroutines outlived Shutdown")
}
