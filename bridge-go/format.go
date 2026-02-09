package main

import (
	"fmt"
	"regexp"
	"strings"
)

// markdownToTelegramHTML converts standard Markdown to Telegram-supported HTML.
// Processing order: code blocks → inline code → escape → inline formatting.
func markdownToTelegramHTML(md string) string {
	var placeholders []string
	placeholder := func(content string) string {
		idx := len(placeholders)
		placeholders = append(placeholders, content)
		return fmt.Sprintf("\x00PH%d\x00", idx)
	}

	out := md

	// 1. Extract fenced code blocks
	codeBlockRe := regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")
	out = codeBlockRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := codeBlockRe.FindStringSubmatch(match)
		lang := parts[1]
		code := strings.TrimSuffix(parts[2], "\n")
		escaped := escapeHTML(code)
		cls := ""
		if lang != "" {
			cls = fmt.Sprintf(` class="language-%s"`, lang)
		}
		return placeholder(fmt.Sprintf("<pre><code%s>%s</code></pre>", cls, escaped))
	})

	// 2. Extract inline code
	inlineCodeRe := regexp.MustCompile("`([^`\n]+)`")
	out = inlineCodeRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := inlineCodeRe.FindStringSubmatch(match)
		return placeholder(fmt.Sprintf("<code>%s</code>", escapeHTML(parts[1])))
	})

	// 3. Escape HTML entities in non-code text
	phSplitRe := regexp.MustCompile(`(\x00PH\d+\x00)`)
	phMatchRe := regexp.MustCompile(`^\x00PH\d+\x00$`)
	segments := phSplitRe.Split(out, -1)
	placeholderMatches := phSplitRe.FindAllString(out, -1)

	var rebuilt strings.Builder
	for i, seg := range segments {
		rebuilt.WriteString(escapeHTML(seg))
		if i < len(placeholderMatches) {
			rebuilt.WriteString(placeholderMatches[i])
		}
	}
	out = rebuilt.String()

	// 4. Headings → bold
	headingRe := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	out = headingRe.ReplaceAllString(out, "<b>$1</b>")

	// 5. Bold: **text**
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
	out = boldRe.ReplaceAllString(out, "<b>$1</b>")

	// 6. Italic: *text*
	italicRe := regexp.MustCompile(`(?:^|[^*])\*([^*]+)\*(?:[^*]|$)`)
	// Simpler approach: after bold is consumed, remaining single * is italic
	singleStarRe := regexp.MustCompile(`\*([^*\n]+)\*`)
	out = singleStarRe.ReplaceAllString(out, "<i>$1</i>")

	// 7. Strikethrough: ~~text~~
	strikeRe := regexp.MustCompile(`~~(.+?)~~`)
	out = strikeRe.ReplaceAllString(out, "<s>$1</s>")

	// 8. Links: [text](url)
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	out = linkRe.ReplaceAllString(out, `<a href="$2">$1</a>`)

	// 9. Blockquotes (after HTML escaping, > becomes &gt;)
	blockquoteRe := regexp.MustCompile(`(?m)(?:^&gt; .+\n?)+`)
	out = blockquoteRe.ReplaceAllStringFunc(out, func(block string) string {
		lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
		var cleaned []string
		for _, l := range lines {
			cleaned = append(cleaned, strings.TrimPrefix(l, "&gt; "))
		}
		return fmt.Sprintf("<blockquote>%s</blockquote>", strings.Join(cleaned, "\n"))
	})

	// 10. Restore placeholders
	phRestoreRe := regexp.MustCompile(`\x00PH(\d+)\x00`)
	out = phRestoreRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := phRestoreRe.FindStringSubmatch(match)
		idx := 0
		fmt.Sscanf(parts[1], "%d", &idx)
		if idx < len(placeholders) {
			return placeholders[idx]
		}
		return match
	})

	_ = phMatchRe  // used above in concept
	_ = italicRe   // replaced by simpler approach

	return out
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// parseResponse applies format mode and converts to Telegram HTML.
func parseResponse(text, mode string) []string {
	voiceLine := extractVoiceLine(text)
	taskSummary := extractTaskSummary(text)

	var content string
	switch mode {
	case "concise":
		if voiceLine != "" {
			content = voiceLine
			if taskSummary != "" {
				content += "\n\n📋 " + taskSummary
			}
		} else {
			content = text
		}
	case "voice-only":
		if voiceLine != "" {
			content = voiceLine
		} else {
			content = text
		}
	default: // "full"
		content = text
	}

	content = markdownToTelegramHTML(content)
	return chunkForTelegram(content, 4000)
}

var voiceLineRe = regexp.MustCompile(`(?m)🗣️\s*(?:PAI|Ghost):\s*(.+?)$`)

func extractVoiceLine(text string) string {
	match := voiceLineRe.FindStringSubmatch(text)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

var taskLineRe = regexp.MustCompile(`(?m)^#\d+\s+\[(completed|pending|in_progress)\]`)

func extractTaskSummary(text string) string {
	matches := taskLineRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return ""
	}
	completed := 0
	for _, m := range matches {
		if strings.Contains(m, "completed") {
			completed++
		}
	}
	return fmt.Sprintf("%d/%d tasks passed", completed, len(matches))
}

func chunkForTelegram(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= limit {
			chunks = append(chunks, remaining)
			break
		}

		splitAt := strings.LastIndex(remaining[:limit], "\n\n")
		if splitAt == -1 || splitAt < limit*3/10 {
			splitAt = strings.LastIndex(remaining[:limit], "\n")
		}
		if splitAt == -1 || splitAt < limit*3/10 {
			splitAt = limit
		}

		chunks = append(chunks, remaining[:splitAt])
		remaining = strings.TrimLeft(remaining[splitAt:], " \n")
	}

	return chunks
}
