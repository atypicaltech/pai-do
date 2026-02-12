package main

import (
	"testing"
)

func TestExtractVoiceDirective(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClean string
		wantVoice string
	}{
		{
			name:      "VOICE: directive",
			input:     "Some text\nVOICE: Hello there!\nMore text",
			wantClean: "Some text\nMore text",
			wantVoice: "Hello there!",
		},
		{
			name:      "🗣️ PAI: voice line",
			input:     "Some text\n🗣️ PAI: Deploy complete.\nMore text",
			wantClean: "Some text\nMore text",
			wantVoice: "Deploy complete.",
		},
		{
			name:      "🗣️ Ghost: voice line",
			input:     "Some text\n🗣️ Ghost: I found something.\nMore text",
			wantClean: "Some text\nMore text",
			wantVoice: "I found something.",
		},
		{
			name:      "🗣️ Kai: custom daidentity name",
			input:     "Some text\n🗣️ Kai: Custom name works.\nMore text",
			wantClean: "Some text\nMore text",
			wantVoice: "Custom name works.",
		},
		{
			name:      "🗣️ Jarvis: another custom name",
			input:     "Result done\n🗣️ Jarvis: All systems operational.",
			wantClean: "Result done",
			wantVoice: "All systems operational.",
		},
		{
			name:      "no voice directive",
			input:     "Just regular text\nNothing special here",
			wantClean: "Just regular text\nNothing special here",
			wantVoice: "",
		},
		{
			name:      "only first voice line used",
			input:     "VOICE: First line\nVOICE: Second line",
			wantClean: "",
			wantVoice: "First line",
		},
		{
			name:      "VOICE: and 🗣️ PAI: mixed - first wins",
			input:     "VOICE: From directive\n🗣️ PAI: From algorithm",
			wantClean: "",
			wantVoice: "From directive",
		},
		{
			name:      "🗣️ PAI: first, VOICE: second - first wins",
			input:     "🗣️ PAI: From algorithm\nVOICE: From directive",
			wantClean: "",
			wantVoice: "From algorithm",
		},
		{
			name:      "voice line with leading whitespace",
			input:     "  🗣️ PAI: Indented voice line.",
			wantClean: "",
			wantVoice: "Indented voice line.",
		},
		{
			name:      "VOICE: not a prefix - no match",
			input:     "The VOICE: directive is cool",
			wantClean: "The VOICE: directive is cool",
			wantVoice: "",
		},
		{
			name:      "🗣️ without name colon - no match",
			input:     "🗣️ just some emoji text",
			wantClean: "🗣️ just some emoji text",
			wantVoice: "",
		},
		{
			name:      "empty input",
			input:     "",
			wantClean: "",
			wantVoice: "",
		},
		{
			name:      "voice line only",
			input:     "VOICE: Just voice, nothing else",
			wantClean: "",
			wantVoice: "Just voice, nothing else",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotVoice := extractVoiceDirective(tt.input)
			if gotClean != tt.wantClean {
				t.Errorf("clean text:\n  got:  %q\n  want: %q", gotClean, tt.wantClean)
			}
			if gotVoice != tt.wantVoice {
				t.Errorf("voice text:\n  got:  %q\n  want: %q", gotVoice, tt.wantVoice)
			}
		})
	}
}

func TestExtractSendDirectives(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClean string
		wantPaths []string
	}{
		{
			name:      "single SEND directive",
			input:     "Here's the file\nSEND: /tmp/image.png\nDone",
			wantClean: "Here's the file\nDone",
			wantPaths: []string{"/tmp/image.png"},
		},
		{
			name:      "multiple SEND directives",
			input:     "SEND: /tmp/a.png\nSEND: /tmp/b.pdf",
			wantClean: "",
			wantPaths: []string{"/tmp/a.png", "/tmp/b.pdf"},
		},
		{
			name:      "no SEND directives",
			input:     "Just regular text mentioning /tmp/file.txt",
			wantClean: "Just regular text mentioning /tmp/file.txt",
			wantPaths: nil,
		},
		{
			name:      "SEND not at line start - no match",
			input:     "Use SEND: /tmp/file.txt to send",
			wantClean: "Use SEND: /tmp/file.txt to send",
			wantPaths: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClean, gotPaths := extractSendDirectives(tt.input)
			if gotClean != tt.wantClean {
				t.Errorf("clean text:\n  got:  %q\n  want: %q", gotClean, tt.wantClean)
			}
			if len(gotPaths) != len(tt.wantPaths) {
				t.Errorf("paths count: got %d, want %d", len(gotPaths), len(tt.wantPaths))
				return
			}
			for i, p := range gotPaths {
				if p != tt.wantPaths[i] {
					t.Errorf("path[%d]: got %q, want %q", i, p, tt.wantPaths[i])
				}
			}
		})
	}
}
