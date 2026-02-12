package main

import (
	"strings"
	"testing"
)

func TestMarkdownToTelegramHTML_Bold(t *testing.T) {
	got := markdownToTelegramHTML("**hello**")
	if got != "<b>hello</b>" {
		t.Errorf("bold: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Italic(t *testing.T) {
	got := markdownToTelegramHTML("*hello*")
	if got != "<i>hello</i>" {
		t.Errorf("italic: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_InlineCode(t *testing.T) {
	got := markdownToTelegramHTML("use `fmt.Println`")
	if !strings.Contains(got, "<code>fmt.Println</code>") {
		t.Errorf("inline code: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_CodeBlock(t *testing.T) {
	input := "```go\nfmt.Println(\"hi\")\n```"
	got := markdownToTelegramHTML(input)
	if !strings.Contains(got, `<pre><code class="language-go">`) {
		t.Errorf("code block missing language class: got %q", got)
	}
	if !strings.Contains(got, "fmt.Println") {
		t.Errorf("code block missing content: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_CodeBlockNoLang(t *testing.T) {
	input := "```\nhello\n```"
	got := markdownToTelegramHTML(input)
	if !strings.Contains(got, "<pre><code>hello</code></pre>") {
		t.Errorf("code block no lang: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_HTMLEscaping(t *testing.T) {
	got := markdownToTelegramHTML("x < y & z > w")
	if !strings.Contains(got, "&lt;") || !strings.Contains(got, "&amp;") || !strings.Contains(got, "&gt;") {
		t.Errorf("HTML escaping: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_CodeBlockPreservesSpecialChars(t *testing.T) {
	input := "```\nif x < 10 && y > 5 {\n```"
	got := markdownToTelegramHTML(input)
	if !strings.Contains(got, "&lt;") || !strings.Contains(got, "&amp;&amp;") {
		t.Errorf("code block escaping: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Heading(t *testing.T) {
	got := markdownToTelegramHTML("## Title Here")
	if got != "<b>Title Here</b>" {
		t.Errorf("heading: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Link(t *testing.T) {
	got := markdownToTelegramHTML("[click](https://example.com)")
	if !strings.Contains(got, `<a href="https://example.com">click</a>`) {
		t.Errorf("link: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Strikethrough(t *testing.T) {
	got := markdownToTelegramHTML("~~removed~~")
	if got != "<s>removed</s>" {
		t.Errorf("strikethrough: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Blockquote(t *testing.T) {
	got := markdownToTelegramHTML("> quoted text")
	if !strings.Contains(got, "<blockquote>quoted text</blockquote>") {
		t.Errorf("blockquote: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_PlainText(t *testing.T) {
	got := markdownToTelegramHTML("just plain text")
	if got != "just plain text" {
		t.Errorf("plain text: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Mixed(t *testing.T) {
	input := "**bold** and *italic* and `code`"
	got := markdownToTelegramHTML(input)
	if !strings.Contains(got, "<b>bold</b>") {
		t.Errorf("mixed bold: got %q", got)
	}
	if !strings.Contains(got, "<i>italic</i>") {
		t.Errorf("mixed italic: got %q", got)
	}
	if !strings.Contains(got, "<code>code</code>") {
		t.Errorf("mixed code: got %q", got)
	}
}

func TestChunkForTelegram(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		limit      int
		wantChunks int
	}{
		{
			name:       "short text fits in one chunk",
			text:       "Hello world",
			limit:      100,
			wantChunks: 1,
		},
		{
			name:       "exact limit fits in one chunk",
			text:       "12345",
			limit:      5,
			wantChunks: 1,
		},
		{
			name:       "splits on paragraph break",
			text:       "First paragraph.\n\nSecond paragraph.",
			limit:      20,
			wantChunks: 2,
		},
		{
			name:       "splits on newline if no paragraph break",
			text:       "Line one\nLine two\nLine three",
			limit:      15,
			wantChunks: 3,
		},
		{
			name:       "empty text",
			text:       "",
			limit:      100,
			wantChunks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkForTelegram(tt.text, tt.limit)
			if len(chunks) != tt.wantChunks {
				t.Errorf("got %d chunks, want %d. chunks: %v", len(chunks), tt.wantChunks, chunks)
			}
			// Verify no chunk exceeds limit
			for i, c := range chunks {
				if len(c) > tt.limit {
					t.Errorf("chunk[%d] length %d exceeds limit %d", i, len(c), tt.limit)
				}
			}
		})
	}
}

func TestChunkForTelegram_ContentPreserved(t *testing.T) {
	original := "Part one.\n\nPart two.\n\nPart three."
	chunks := chunkForTelegram(original, 15)
	rejoined := strings.Join(chunks, "\n\n")
	// All content words should be present
	for _, word := range []string{"Part one.", "Part two.", "Part three."} {
		if !strings.Contains(rejoined, word) {
			t.Errorf("missing content %q in rejoined chunks: %q", word, rejoined)
		}
	}
}

func TestExtractVoiceLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "PAI voice line",
			input: "Some output\n🗣️ PAI: Deploy complete.",
			want:  "Deploy complete.",
		},
		{
			name:  "Ghost voice line",
			input: "🗣️ Ghost: Found the bug.",
			want:  "Found the bug.",
		},
		{
			name:  "no voice line",
			input: "Just regular text",
			want:  "",
		},
		{
			name:  "voice line in middle of text",
			input: "Before\n🗣️ PAI: Middle voice.\nAfter",
			want:  "Middle voice.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVoiceLine(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTaskSummary(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "3 of 4 completed",
			input: "#1 [completed] Test passes\n#2 [completed] Build works\n#3 [pending] Deploy\n#4 [completed] Docs",
			want:  "3/4 tasks passed",
		},
		{
			name:  "all completed",
			input: "#1 [completed] A\n#2 [completed] B",
			want:  "2/2 tasks passed",
		},
		{
			name:  "none completed",
			input: "#1 [pending] A\n#2 [in_progress] B",
			want:  "0/2 tasks passed",
		},
		{
			name:  "no tasks",
			input: "Just regular text",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTaskSummary(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResponse_FullMode(t *testing.T) {
	input := "Full response text here"
	chunks := parseResponse(input, "full")
	if len(chunks) != 1 || !strings.Contains(chunks[0], "Full response text here") {
		t.Errorf("full mode: got %v", chunks)
	}
}

func TestParseResponse_ConciseMode_WithVoice(t *testing.T) {
	input := "Long output\n🗣️ PAI: Short summary."
	chunks := parseResponse(input, "concise")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	// Concise mode should only show the voice line content
	if strings.Contains(chunks[0], "Long output") {
		t.Error("concise mode should not include full text when voice line exists")
	}
	if !strings.Contains(chunks[0], "Short summary.") {
		t.Errorf("concise mode should include voice line: got %q", chunks[0])
	}
}

func TestParseResponse_ConciseMode_NoVoice(t *testing.T) {
	input := "Full response without voice line"
	chunks := parseResponse(input, "concise")
	if len(chunks) != 1 || !strings.Contains(chunks[0], "Full response without voice line") {
		t.Errorf("concise without voice should fall back to full text: got %v", chunks)
	}
}

func TestParseResponse_VoiceOnlyMode(t *testing.T) {
	input := "Long output\n🗣️ PAI: Voice only."
	chunks := parseResponse(input, "voice-only")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if strings.Contains(chunks[0], "Long output") {
		t.Error("voice-only mode should not include full text")
	}
	if !strings.Contains(chunks[0], "Voice only.") {
		t.Errorf("voice-only mode missing voice line: got %q", chunks[0])
	}
}
