package main

import (
	"strings"
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

func TestIsTextExt(t *testing.T) {
	textExts := []string{"txt", "md", "csv", "json", "xml", "html", "yml", "yaml",
		"toml", "ini", "log", "py", "js", "ts", "sh", "rb", "go", "rs",
		"java", "c", "cpp", "h", "css", "sql"}

	for _, ext := range textExts {
		if !isTextExt(ext) {
			t.Errorf("%q should be a text extension", ext)
		}
	}

	nonTextExts := []string{"png", "jpg", "gif", "pdf", "zip", "exe", "mp3", "mp4", "bin", ""}
	for _, ext := range nonTextExts {
		if isTextExt(ext) {
			t.Errorf("%q should NOT be a text extension", ext)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"ab", 1, "a..."},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestImageExtRe(t *testing.T) {
	matches := []string{"/tmp/photo.png", "/tmp/photo.jpg", "/tmp/photo.jpeg",
		"/tmp/photo.gif", "/tmp/photo.webp", "/tmp/PHOTO.PNG", "/tmp/photo.JPG"}

	for _, path := range matches {
		if !imageExtRe.MatchString(path) {
			t.Errorf("%q should match image regex", path)
		}
	}

	nonMatches := []string{"/tmp/file.pdf", "/tmp/file.txt", "/tmp/file.mp4", "/tmp/file"}
	for _, path := range nonMatches {
		if imageExtRe.MatchString(path) {
			t.Errorf("%q should NOT match image regex", path)
		}
	}
}

func TestExtractSendDirectives_HomeTilde(t *testing.T) {
	_, paths := extractSendDirectives("SEND: ~/documents/file.pdf")
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	// Should be expanded (not start with ~)
	if strings.HasPrefix(paths[0], "~/") {
		t.Errorf("path should be expanded: got %q", paths[0])
	}
}

func TestIsSafeSendPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		// Allowed paths
		{"project file", "/mnt/pai-data/projects/repo/file.txt", true},
		{"memory file", "/mnt/pai-data/memory/notes.md", true},
		{"tmp file", "/tmp/output.png", true},
		{"home file", "/home/pai/document.pdf", true},

		// Blocked: outside allowed trees
		{"etc secrets", "/etc/pai/secrets.env", false},
		{"etc shadow", "/etc/shadow", false},
		{"root ssh", "/root/.ssh/id_rsa", false},
		{"usr bin", "/usr/local/bin/pai-bridge", false},

		// Blocked: denied substrings even if inside allowed tree
		{"secrets.env in projects", "/mnt/pai-data/projects/secrets.env", false},
		{".ssh in home", "/home/pai/.ssh/id_rsa", false},
		{".env in projects", "/mnt/pai-data/projects/.env", false},
		{"credentials in home", "/home/pai/credentials.json", false},
		{"token file in tmp", "/tmp/token.txt", false},
		{".key in projects", "/mnt/pai-data/projects/server.key", false},
		{".pem in tmp", "/tmp/cert.pem", false},

		// Edge cases
		{"bare allowed prefix", "/mnt/pai-data/projects", true},
		{"near-miss prefix", "/mnt/pai-data/project-other/file.txt", false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeSendPath(tt.path)
			if got != tt.want {
				t.Errorf("isSafeSendPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
