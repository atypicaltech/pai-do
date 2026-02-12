package main

import (
	"encoding/json"
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
