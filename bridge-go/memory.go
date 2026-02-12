package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MemoryManager struct {
	basePath      string
	enabled       bool
	retentionDays int
}

type ConversationTurn struct {
	Timestamp int64  `json:"ts"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	SessionID string `json:"sessionId"`
}

func NewMemoryManager(cfg *Config) *MemoryManager {
	return &MemoryManager{
		basePath:      cfg.Memory.BasePath,
		enabled:       cfg.Memory.Enabled,
		retentionDays: cfg.Memory.RetentionDays,
	}
}

// LogTurn appends a single conversation turn to the session's JSONL log file.
// Path: {basePath}/conversations/{userID}/{sessionID}.jsonl
func (mm *MemoryManager) LogTurn(userID, sessionID, role, text string) {
	if !mm.enabled || text == "" {
		return
	}

	dir := filepath.Join(mm.basePath, "conversations", userID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[PAI Memory] Failed to create dir %s: %v", dir, err)
		return
	}

	logPath := filepath.Join(dir, sessionID+".jsonl")

	turn := ConversationTurn{
		Timestamp: time.Now().UnixMilli(),
		Role:      role,
		Text:      text,
		SessionID: sessionID,
	}

	data, err := json.Marshal(turn)
	if err != nil {
		log.Printf("[PAI Memory] Failed to marshal turn: %v", err)
		return
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[PAI Memory] Failed to open log %s: %v", logPath, err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "%s\n", data)
}

// ReadConversationLog reads all turns from a session's JSONL log file
// and returns them as a formatted string for summarization.
func (mm *MemoryManager) ReadConversationLog(userID, sessionID string) (string, error) {
	logPath := filepath.Join(mm.basePath, "conversations", userID, sessionID+".jsonl")

	f, err := os.Open(logPath)
	if err != nil {
		return "", fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024) // 512KB buffer for long responses

	for scanner.Scan() {
		var turn ConversationTurn
		if err := json.Unmarshal(scanner.Bytes(), &turn); err != nil {
			continue
		}
		// Truncate very long turns to keep the summary prompt manageable
		text := turn.Text
		if len(text) > 2000 {
			text = text[:2000] + "... [truncated]"
		}
		fmt.Fprintf(&sb, "[%s]: %s\n\n", turn.Role, text)
	}

	return sb.String(), scanner.Err()
}

const flushPrompt = `You are summarizing a conversation for future context continuity.
Given the following conversation log between a user and an AI assistant, produce a concise summary with these sections:

## Summary
- 3-5 bullet points of what was discussed and accomplished

## Decisions
- Any decisions made or preferences expressed (skip if none)

## Open Items
- Any unfinished tasks or open questions (skip if none)

Output ONLY the summary in markdown, no preamble or explanation.

--- CONVERSATION LOG ---
`

// FlushSession reads the conversation log for a session, spawns a Claude
// subprocess to summarize it, and writes the summary to a durable file.
// Path: {basePath}/summaries/{userID}/{date}-{sessionID[:8]}.md
func (mm *MemoryManager) FlushSession(userID, sessionID, model string) {
	if !mm.enabled {
		return
	}

	log.Printf("[PAI Memory] Flushing session %s for user %s", sessionID[:8], userID)

	// Read the conversation log
	conversationLog, err := mm.ReadConversationLog(userID, sessionID)
	if err != nil {
		log.Printf("[PAI Memory] No conversation log to flush for %s: %v", sessionID[:8], err)
		return
	}

	if strings.TrimSpace(conversationLog) == "" {
		log.Printf("[PAI Memory] Empty conversation log for %s, skipping flush", sessionID[:8])
		return
	}

	// Build the summarization prompt
	prompt := flushPrompt + conversationLog

	// Resolve claude binary
	claudePath := os.Getenv("CLAUDE_PATH")
	if claudePath == "" {
		home, _ := os.UserHomeDir()
		claudePath = filepath.Join(home, ".local/bin/claude")
	}
	if resolved, err := filepath.EvalSymlinks(claudePath); err == nil {
		claudePath = resolved
	}

	// Spawn Claude with a 2-minute timeout for summarization
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, claudePath, "-p", prompt, "--model", model, "--output-format", "text")
	cmd.Env = os.Environ()

	output, err := cmd.Output()
	summary := strings.TrimSpace(string(output))

	if err != nil || summary == "" {
		if err != nil {
			log.Printf("[PAI Memory] Claude summarization failed for %s: %v — writing raw fallback", sessionID[:8], err)
		} else {
			log.Printf("[PAI Memory] Empty summary for %s — writing raw fallback", sessionID[:8])
		}
		summary = mm.rawFallbackSummary(conversationLog)
		if summary == "" {
			return
		}
	}

	// Write the summary file
	dir := filepath.Join(mm.basePath, "summaries", userID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[PAI Memory] Failed to create summaries dir: %v", err)
		return
	}

	date := time.Now().Format("2006-01-02")
	shortID := sessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	summaryPath := filepath.Join(dir, fmt.Sprintf("%s-%s.md", date, shortID))

	if err := os.WriteFile(summaryPath, []byte(summary+"\n"), 0644); err != nil {
		log.Printf("[PAI Memory] Failed to write summary: %v", err)
		return
	}

	log.Printf("[PAI Memory] Session %s flushed to %s", shortID, summaryPath)

	// Extract first summary bullet as a daily note
	for _, line := range strings.Split(summary, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "- Any ") {
			mm.AppendDailyNote(userID, strings.TrimPrefix(trimmed, "- "))
			break
		}
	}
}

// rawFallbackSummary extracts the last few turns from a conversation log
// as a fallback when Claude summarization fails.
func (mm *MemoryManager) rawFallbackSummary(conversationLog string) string {
	lines := strings.Split(strings.TrimSpace(conversationLog), "\n\n")
	// Take last 6 turns (3 exchanges)
	start := 0
	if len(lines) > 6 {
		start = len(lines) - 6
	}
	recent := lines[start:]

	var sb strings.Builder
	sb.WriteString("## Summary (raw — summarization failed)\n")
	for _, line := range recent {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Truncate long turns
		if len(trimmed) > 300 {
			trimmed = trimmed[:300] + "..."
		}
		sb.WriteString("- ")
		sb.WriteString(trimmed)
		sb.WriteString("\n")
	}
	return sb.String()
}

// GetRecentContext reads the most recent summary files for a user and
// returns them as a formatted context block for injection into new sessions.
func (mm *MemoryManager) GetRecentContext(userID string, maxSummaries int) string {
	if !mm.enabled {
		return ""
	}

	dir := filepath.Join(mm.basePath, "summaries", userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "" // No summaries yet — normal for first-time users
	}

	// Filter to .md files only
	var mdFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			mdFiles = append(mdFiles, e.Name())
		}
	}

	if len(mdFiles) == 0 {
		return ""
	}

	// Sort by filename (date-prefixed, so alphabetical = chronological)
	sort.Strings(mdFiles)

	// Take the most recent N
	if len(mdFiles) > maxSummaries {
		mdFiles = mdFiles[len(mdFiles)-maxSummaries:]
	}

	var sb strings.Builder
	sb.WriteString("[PREVIOUS SESSION CONTEXT]\n")
	sb.WriteString("These are summaries from your recent conversations with this user.\n")
	sb.WriteString("Use them to maintain continuity — reference prior decisions and open items.\n\n")

	for _, name := range mdFiles {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		// Extract date from filename (format: 2006-01-02-sessionid.md)
		date := strings.SplitN(name, "-", 4)
		label := name
		if len(date) >= 3 {
			label = strings.Join(date[:3], "-")
		}
		fmt.Fprintf(&sb, "--- Session %s ---\n%s\n\n", label, strings.TrimSpace(string(content)))
	}

	sb.WriteString("[END PREVIOUS SESSION CONTEXT]\n\n")
	return sb.String()
}

// AppendDailyNote appends a timestamped note to today's daily log for a user.
// Called during session flush to capture key facts that persist across sessions.
// Path: {basePath}/daily/{userID}/{YYYY-MM-DD}.md
func (mm *MemoryManager) AppendDailyNote(userID, note string) {
	if !mm.enabled || strings.TrimSpace(note) == "" {
		return
	}

	dir := filepath.Join(mm.basePath, "daily", userID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[PAI Memory] Failed to create daily dir: %v", err)
		return
	}

	date := time.Now().Format("2006-01-02")
	dailyPath := filepath.Join(dir, date+".md")

	f, err := os.OpenFile(dailyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[PAI Memory] Failed to open daily log: %v", err)
		return
	}
	defer f.Close()

	ts := time.Now().Format("15:04")
	fmt.Fprintf(f, "- [%s] %s\n", ts, strings.TrimSpace(note))
}

// CleanOldFiles removes memory files past their retention period.
// Retention is proportional to retentionDays: JSONL=1x, daily=2x, summaries=6x.
func (mm *MemoryManager) CleanOldFiles() {
	if !mm.enabled || mm.retentionDays <= 0 {
		return
	}

	now := time.Now()
	cleaned := 0

	// Conversation JSONL logs — delete at 1x retention (default 14 days)
	cleaned += mm.cleanDir(filepath.Join(mm.basePath, "conversations"), now, mm.retentionDays)

	// Daily notes — delete at 2x retention (default 28 days)
	cleaned += mm.cleanDir(filepath.Join(mm.basePath, "daily"), now, mm.retentionDays*2)

	// Summaries — delete at 6x retention (default 84 days)
	cleaned += mm.cleanDir(filepath.Join(mm.basePath, "summaries"), now, mm.retentionDays*6)

	if cleaned > 0 {
		log.Printf("[PAI Memory] Retention cleanup: removed %d old file(s)", cleaned)
	}
}

// cleanDir walks a directory tree and deletes regular files older than maxDays.
func (mm *MemoryManager) cleanDir(dir string, now time.Time, maxDays int) int {
	cutoff := now.AddDate(0, 0, -maxDays)
	cleaned := 0

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				cleaned++
			}
		}
		return nil
	})

	return cleaned
}

// GetDailyNotes reads today's and yesterday's daily notes for a user.
// Returns a formatted context block for injection into new sessions.
func (mm *MemoryManager) GetDailyNotes(userID string) string {
	if !mm.enabled {
		return ""
	}

	dir := filepath.Join(mm.basePath, "daily", userID)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	var sb strings.Builder
	loaded := false

	for _, date := range []string{yesterday, today} {
		content, err := os.ReadFile(filepath.Join(dir, date+".md"))
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		if !loaded {
			sb.WriteString("[DAILY NOTES]\n")
			loaded = true
		}
		label := "Today"
		if date == yesterday {
			label = "Yesterday"
		}
		fmt.Fprintf(&sb, "--- %s (%s) ---\n%s\n\n", label, date, trimmed)
	}

	if loaded {
		sb.WriteString("[END DAILY NOTES]\n\n")
	}
	return sb.String()
}
