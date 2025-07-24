package testutils

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMockSBSMessage(t *testing.T) {
	msgType := 8
	hexIdent := "ABC123"
	
	msg := MockSBSMessage(msgType, hexIdent)
	
	if msg == nil {
		t.Fatal("MockSBSMessage() returned nil")
	}
	
	// Check that the message contains the expected components
	if !strings.Contains(msg.Raw, "MSG") {
		t.Error("Mock message should contain 'MSG'")
	}
	
	if !strings.Contains(msg.Raw, hexIdent) {
		t.Errorf("Mock message should contain hexIdent '%s'", hexIdent)
	}
	
	// Check that the message has the expected format
	parts := strings.Split(msg.Raw, ",")
	if len(parts) < 6 {
		t.Errorf("Mock message should have at least 6 parts, got %d", len(parts))
	}
	
	if parts[0] != "MSG" {
		t.Errorf("First part should be 'MSG', got '%s'", parts[0])
	}
	
	if parts[5] != hexIdent {
		t.Errorf("Sixth part should be hexIdent '%s', got '%s'", hexIdent, parts[5])
	}
	
	// Check timestamp is recent
	if time.Since(msg.Timestamp) > 5*time.Second {
		t.Error("Timestamp should be recent")
	}
	
	// Check source
	if msg.Source != "test-source" {
		t.Errorf("Expected source 'test-source', got '%s'", msg.Source)
	}
}

func TestMockSBSMessage_DifferentTypes(t *testing.T) {
	testCases := []struct {
		msgType  int
		hexIdent string
	}{
		{1, "ABC123"},
		{4, "DEF456"},
		{8, "GHI789"},
		{16, "JKL012"},
	}
	
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Type%d_%s", tc.msgType, tc.hexIdent), func(t *testing.T) {
			msg := MockSBSMessage(tc.msgType, tc.hexIdent)
			
			if msg == nil {
				t.Fatal("MockSBSMessage() returned nil")
			}
			
			parts := strings.Split(msg.Raw, ",")
			if len(parts) < 2 {
				t.Fatal("Message should have at least 2 parts")
			}
			
			// Check message type is in the right position
			if parts[1] != fmt.Sprintf("%d", tc.msgType) {
				t.Errorf("Expected message type %d, got %s", tc.msgType, parts[1])
			}
			
			// Check hex identifier is in the right position
			if parts[5] != tc.hexIdent {
				t.Errorf("Expected hexIdent %s, got %s", tc.hexIdent, parts[5])
			}
		})
	}
}

func TestWaitForCondition_Success(t *testing.T) {
	condition := func() bool {
		return true
	}
	
	err := WaitForCondition(condition, 1*time.Second)
	if err != nil {
		t.Errorf("WaitForCondition() should succeed, got error: %v", err)
	}
}

func TestWaitForCondition_Timeout(t *testing.T) {
	condition := func() bool {
		return false
	}
	
	err := WaitForCondition(condition, 100*time.Millisecond)
	if err == nil {
		t.Error("WaitForCondition() should timeout")
	}
	
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestWaitForCondition_ConditionBecomesTrue(t *testing.T) {
	counter := 0
	condition := func() bool {
		counter++
		return counter >= 3
	}
	
	err := WaitForCondition(condition, 1*time.Second)
	if err != nil {
		t.Errorf("WaitForCondition() should succeed, got error: %v", err)
	}
	
	if counter < 3 {
		t.Errorf("Condition should have been called at least 3 times, got %d", counter)
	}
}

func TestIsIntegrationTest(t *testing.T) {
	result := IsIntegrationTest()
	
	// The function currently always returns true, so we test that
	if !result {
		t.Error("IsIntegrationTest() should return true")
	}
}

func TestMockSBSMessage_EmptyHexIdent(t *testing.T) {
	msg := MockSBSMessage(8, "")
	
	if msg == nil {
		t.Fatal("MockSBSMessage() returned nil")
	}
	
	parts := strings.Split(msg.Raw, ",")
	if parts[5] != "" {
		t.Errorf("Expected empty hexIdent, got '%s'", parts[5])
	}
}

func TestMockSBSMessage_SpecialCharacters(t *testing.T) {
	hexIdent := "ABC-123_DEF"
	msg := MockSBSMessage(8, hexIdent)
	
	if msg == nil {
		t.Fatal("MockSBSMessage() returned nil")
	}
	
	if !strings.Contains(msg.Raw, hexIdent) {
		t.Errorf("Mock message should contain hexIdent '%s'", hexIdent)
	}
} 