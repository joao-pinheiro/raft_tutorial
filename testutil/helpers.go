package testutil

import (
	"fmt"
	"time"
)

// WaitFor polls the condition function until it returns true or the timeout expires.
func WaitFor(timeout time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("condition not met within %v", timeout)
}
