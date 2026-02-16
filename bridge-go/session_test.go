package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestExtractTextFromEvent(t *testing.T) {
	tests := []struct {
		name  string
		event map[string]interface{}
		want  string
	}{
		{
			name: "assistant text block",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "Hello world",
						},
					},
				},
			},
			want: "Hello world",
		},
		{
			name: "multiple text blocks",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "First "},
						map[string]interface{}{"type": "text", "text": "Second"},
					},
				},
			},
			want: "First Second",
		},
		{
			name: "tool_use block ignored",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"type": "tool_use", "name": "Read"},
					},
				},
			},
			want: "",
		},
		{
			name:  "system event returns empty",
			event: map[string]interface{}{"type": "system", "session_id": "abc"},
			want:  "",
		},
		{
			name:  "user event returns empty",
			event: map[string]interface{}{"type": "user"},
			want:  "",
		},
		{
			name: "no content field",
			event: map[string]interface{}{
				"type":    "assistant",
				"message": map[string]interface{}{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromEvent(tt.event)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractCreatedFilesFromEvent(t *testing.T) {
	tests := []struct {
		name  string
		event map[string]interface{}
		want  []string
	}{
		{
			name: "Write tool creates file",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "tool_use",
							"name": "Write",
							"input": map[string]interface{}{
								"file_path": "/tmp/output.txt",
							},
						},
					},
				},
			},
			want: []string{"/tmp/output.txt"},
		},
		{
			name: "Bash redirect",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "tool_use",
							"name": "Bash",
							"input": map[string]interface{}{
								"command": "echo hello > /tmp/out.txt",
							},
						},
					},
				},
			},
			want: []string{"/tmp/out.txt"},
		},
		{
			name: "Bash output flag",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "tool_use",
							"name": "Bash",
							"input": map[string]interface{}{
								"command": "curl -o /tmp/download.json http://example.com",
							},
						},
					},
				},
			},
			want: []string{"/tmp/download.json"},
		},
		{
			name: "non-assistant event",
			event: map[string]interface{}{
				"type": "system",
			},
			want: nil,
		},
		{
			name: "text block - no files",
			event: map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "hello"},
					},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCreatedFilesFromEvent(tt.event)
			if len(got) != len(tt.want) {
				t.Errorf("got %d files %v, want %d files %v", len(got), got, len(tt.want), tt.want)
				return
			}
			for i, f := range got {
				if f != tt.want[i] {
					t.Errorf("file[%d]: got %q, want %q", i, f, tt.want[i])
				}
			}
		})
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  int
	}{
		{
			name:  "add to empty",
			slice: nil,
			item:  "a",
			want:  1,
		},
		{
			name:  "add new item",
			slice: []string{"a", "b"},
			item:  "c",
			want:  3,
		},
		{
			name:  "duplicate not added",
			slice: []string{"a", "b"},
			item:  "a",
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.slice, tt.item)
			if len(got) != tt.want {
				t.Errorf("got len %d, want %d", len(got), tt.want)
			}
		})
	}
}

// TestExtractTextFromEvent_RoundTrip tests with actual JSON parsing to simulate
// real stream-json events from Claude CLI.
func TestExtractTextFromEvent_RoundTrip(t *testing.T) {
	jsonEvent := `{
		"type": "assistant",
		"message": {
			"role": "assistant",
			"content": [
				{"type": "text", "text": "Here is the answer."},
				{"type": "tool_use", "name": "Read", "input": {"file_path": "/tmp/x"}}
			]
		}
	}`

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(jsonEvent), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := extractTextFromEvent(event)
	if got != "Here is the answer." {
		t.Errorf("round-trip: got %q", got)
	}

	files := extractCreatedFilesFromEvent(event)
	if len(files) != 0 {
		t.Errorf("Read tool should not produce created files, got: %v", files)
	}
}

// --- Message queue tests ---

// newTestSessionManager creates a minimal SessionManager suitable for unit
// tests that don't need disk persistence or a real MemoryManager.
func newTestSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		procs:    make(map[string]context.CancelFunc),
		config: &Config{
			Sessions: SessionConfig{
				MaxConcurrent:  2,
				DefaultWorkDir: "/tmp",
				DefaultModel:   "test-model",
			},
		},
		stateDir: "/tmp/pai-test-state",
		memory:   &MemoryManager{enabled: false},
	}
}

func TestBuildBatch_SingleTextMessage(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{Text: "check the logs"},
	}
	text, att := sm.buildBatch(msgs)
	if att != nil {
		t.Error("expected no attachment")
	}
	if !strings.Contains(text, "[While you were working, I sent 1 follow-up message(s):]") {
		t.Errorf("missing header in: %q", text)
	}
	if !strings.Contains(text, "[Follow-up message 1/1]:") {
		t.Errorf("missing message label in: %q", text)
	}
	if !strings.Contains(text, "check the logs") {
		t.Errorf("missing message text in: %q", text)
	}
}

func TestBuildBatch_MultipleTextMessages(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{Text: "also check the logs"},
		{Text: "and restart the service"},
		{Text: "bump the version too"},
	}
	text, att := sm.buildBatch(msgs)
	if att != nil {
		t.Error("expected no attachment")
	}
	if !strings.Contains(text, "3 follow-up message(s)") {
		t.Errorf("header should say 3 messages: %q", text)
	}
	if !strings.Contains(text, "[Follow-up message 1/3]:") {
		t.Errorf("missing label 1/3: %q", text)
	}
	if !strings.Contains(text, "[Follow-up message 2/3]:") {
		t.Errorf("missing label 2/3: %q", text)
	}
	if !strings.Contains(text, "[Follow-up message 3/3]:") {
		t.Errorf("missing label 3/3: %q", text)
	}
	if !strings.Contains(text, "also check the logs") ||
		!strings.Contains(text, "and restart the service") ||
		!strings.Contains(text, "bump the version too") {
		t.Errorf("missing message content: %q", text)
	}
}

func TestBuildBatch_TextFileAttachment(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{
			Text: "review this",
			Attachment: &Attachment{
				Type:        "text-file",
				FileName:    "config.yaml",
				TextContent: "key: value",
			},
		},
	}
	text, att := sm.buildBatch(msgs)
	if att != nil {
		t.Error("text-file attachment should be inlined, not returned as binary")
	}
	if !strings.Contains(text, "--- config.yaml ---") {
		t.Errorf("text-file should be inlined: %q", text)
	}
	if !strings.Contains(text, "key: value") {
		t.Errorf("text-file content missing: %q", text)
	}
}

func TestBuildBatch_TextFileAttachment_DefaultLabel(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{
			Text: "look at this",
			Attachment: &Attachment{
				Type:        "text-file",
				TextContent: "some content",
			},
		},
	}
	text, _ := sm.buildBatch(msgs)
	if !strings.Contains(text, "--- document ---") {
		t.Errorf("should use default label 'document': %q", text)
	}
}

func TestBuildBatch_BinaryAttachment_LastOneWins(t *testing.T) {
	sm := newTestSessionManager()
	img1 := &Attachment{Type: "image", Base64: "aaa", MimeType: "image/png"}
	img2 := &Attachment{Type: "image", Base64: "bbb", MimeType: "image/jpeg"}
	msgs := []pendingMessage{
		{Text: "first image", Attachment: img1},
		{Text: "second image", Attachment: img2},
	}
	text, att := sm.buildBatch(msgs)
	if att == nil {
		t.Fatal("expected binary attachment")
	}
	if att.Base64 != "bbb" {
		t.Errorf("should keep last binary attachment, got Base64=%q", att.Base64)
	}
	if !strings.Contains(text, "first image") || !strings.Contains(text, "second image") {
		t.Errorf("text messages should still be included: %q", text)
	}
}

func TestBuildBatch_AllEmptyMessages(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{Text: ""},
		{Text: ""},
		{Text: ""},
	}
	text, att := sm.buildBatch(msgs)
	if text != "" {
		t.Errorf("expected empty string for all-empty batch, got: %q", text)
	}
	if att != nil {
		t.Error("expected nil attachment for all-empty batch")
	}
}

func TestBuildBatch_MixedEmptyAndNonEmpty(t *testing.T) {
	sm := newTestSessionManager()
	msgs := []pendingMessage{
		{Text: ""},
		{Text: "actual message"},
		{Text: ""},
	}
	text, _ := sm.buildBatch(msgs)
	if text == "" {
		t.Error("should not be empty when at least one message has text")
	}
	if !strings.Contains(text, "actual message") {
		t.Errorf("missing non-empty message: %q", text)
	}
	// The empty messages should be skipped (no empty [Follow-up message] entries)
	if strings.Contains(text, "[Follow-up message 1/3]:") {
		t.Errorf("empty message 1 should not appear: %q", text)
	}
	if !strings.Contains(text, "[Follow-up message 2/3]:") {
		t.Errorf("non-empty message 2 should appear: %q", text)
	}
}

func TestBuildBatch_EmptyTextWithBinaryAttachment(t *testing.T) {
	sm := newTestSessionManager()
	img := &Attachment{Type: "image", Base64: "data", MimeType: "image/png"}
	msgs := []pendingMessage{
		{Text: "", Attachment: img},
	}
	text, att := sm.buildBatch(msgs)
	// Text is empty but there's a binary attachment — should NOT return empty
	if att == nil {
		t.Error("should return the binary attachment")
	}
	// The text part may just be the header since the text itself was empty
	// but the function should not return "", nil
	if text == "" && att == nil {
		t.Error("should not skip follow-up when there's a binary attachment")
	}
}

func TestDrainPending_ClearsQueue(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{ID: "test-session-id"}
	session.pending = []pendingMessage{
		{Text: "msg1"},
		{Text: "msg2"},
		{Text: "msg3"},
	}

	sm.drainPending(session)

	if len(session.pending) != 0 {
		t.Errorf("pending should be empty after drain, got %d", len(session.pending))
	}
}

func TestDrainPending_NilQueue(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{ID: "test-session-id"}
	// pending is nil by default — should not panic
	sm.drainPending(session)
	if session.pending != nil {
		t.Error("pending should remain nil")
	}
}

func TestDrainPending_EmptyQueue(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{ID: "test-session-id"}
	session.pending = []pendingMessage{}
	sm.drainPending(session)
	if len(session.pending) != 0 {
		t.Errorf("pending should be empty, got %d", len(session.pending))
	}
}

func TestQueueDepthCap(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{
		ID:     "test-session-id",
		UserID: "user1",
		Status: "busy",
	}
	sm.sessions["user1"] = session

	// Fill to capacity
	for i := 0; i < maxPendingMessages; i++ {
		result, err := sm.SendMessage("user1", fmt.Sprintf("msg %d", i), nil)
		if err != nil {
			t.Fatalf("message %d should queue successfully: %v", i, err)
		}
		if result.Queued != i+1 {
			t.Errorf("message %d: expected Queued=%d, got %d", i, i+1, result.Queued)
		}
	}

	// The next one should be rejected
	_, err := sm.SendMessage("user1", "one too many", nil)
	if err == nil {
		t.Fatal("expected error when queue is full")
	}
	if !strings.Contains(err.Error(), "Too many queued messages") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Verify queue depth is exactly maxPendingMessages
	session.pendingMu.Lock()
	depth := len(session.pending)
	session.pendingMu.Unlock()
	if depth != maxPendingMessages {
		t.Errorf("queue depth should be %d, got %d", maxPendingMessages, depth)
	}
}

func TestQueueDepthCap_Value(t *testing.T) {
	if maxPendingMessages != 20 {
		t.Errorf("maxPendingMessages should be 20, got %d", maxPendingMessages)
	}
}

// TestConcurrentQueueSafety spawns many goroutines that all try to queue
// messages on a busy session simultaneously. The -race flag (enabled in CI)
// will catch any data races.
func TestConcurrentQueueSafety(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{
		ID:     "test-session-id",
		UserID: "user1",
		Status: "busy",
	}
	sm.sessions["user1"] = session

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, err := sm.SendMessage("user1", fmt.Sprintf("concurrent msg %d", n), nil)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Some messages may have been rejected due to queue cap — that's fine.
	// What matters is no panics or data races.
	session.pendingMu.Lock()
	depth := len(session.pending)
	session.pendingMu.Unlock()

	rejections := 0
	for err := range errors {
		if strings.Contains(err.Error(), "Too many queued messages") {
			rejections++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if depth > maxPendingMessages {
		t.Errorf("queue exceeded cap: got %d, max %d", depth, maxPendingMessages)
	}

	expectedQueued := goroutines - rejections
	if depth != expectedQueued {
		t.Errorf("queue depth %d doesn't match expected %d (goroutines=%d, rejections=%d)",
			depth, expectedQueued, goroutines, rejections)
	}

	t.Logf("queued: %d, rejected: %d (cap: %d)", depth, rejections, maxPendingMessages)
}

// TestConcurrentQueueAndDrain tests queue + drain happening simultaneously.
func TestConcurrentQueueAndDrain(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{
		ID:     "test-session-id",
		UserID: "user1",
		Status: "busy",
	}
	sm.sessions["user1"] = session

	const producers = 20
	var wg sync.WaitGroup
	wg.Add(producers + 1)

	// Producers: queue messages
	for i := 0; i < producers; i++ {
		go func(n int) {
			defer wg.Done()
			sm.SendMessage("user1", fmt.Sprintf("msg %d", n), nil)
		}(i)
	}

	// Consumer: drain concurrently
	go func() {
		defer wg.Done()
		sm.drainPending(session)
	}()

	wg.Wait()
	// No panic or race = pass. The queue may or may not be empty
	// depending on timing, which is fine.
}

// TestQueuedMessagePreservesAttachment verifies that queued messages
// retain their attachment data through queue → drain → buildBatch.
func TestQueuedMessagePreservesAttachment(t *testing.T) {
	sm := newTestSessionManager()
	session := &Session{
		ID:     "test-session-id",
		UserID: "user1",
		Status: "busy",
	}
	sm.sessions["user1"] = session

	pdf := &Attachment{
		Type:     "document",
		Base64:   "JVBERi0xLjQ=",
		MimeType: "application/pdf",
		FileName: "report.pdf",
	}

	result, err := sm.SendMessage("user1", "review this PDF", pdf)
	if err != nil {
		t.Fatalf("queue failed: %v", err)
	}
	if result.Queued != 1 {
		t.Fatalf("expected Queued=1, got %d", result.Queued)
	}

	// Simulate drain
	session.pendingMu.Lock()
	queued := session.pending
	session.pending = nil
	session.pendingMu.Unlock()

	text, att := sm.buildBatch(queued)
	if att == nil {
		t.Fatal("binary attachment should be preserved through queue")
	}
	if att.Base64 != "JVBERi0xLjQ=" {
		t.Errorf("attachment Base64 corrupted: %q", att.Base64)
	}
	if att.MimeType != "application/pdf" {
		t.Errorf("attachment MimeType corrupted: %q", att.MimeType)
	}
	if !strings.Contains(text, "review this PDF") {
		t.Errorf("text should be in batch: %q", text)
	}
}
