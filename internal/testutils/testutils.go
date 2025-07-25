package testutils

import (
	"context"
	"fmt"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// MockSBSMessage creates a mock SBS message for testing
func MockSBSMessage(msgType int, hexIdent string) *types.SBSMessage {
	return &types.SBSMessage{
		Raw:       fmt.Sprintf("MSG,%d,111,11111,111111,%s,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111", msgType, hexIdent),
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(condition func() bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for condition")
		case <-ticker.C:
			if condition() {
				return nil
			}
		}
	}
}

// IsIntegrationTest returns true if integration tests are enabled
func IsIntegrationTest() bool {
	return true // This can be controlled by build tags
}
