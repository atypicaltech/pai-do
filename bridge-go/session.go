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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID              string `json:"id"`
	UserID          string `json:"userId"`
	ChatID          string `json:"chatId"`
	WorkDir         string `json:"workDir"`
	Model           string `json:"model"`
	CreatedAt       int64  `json:"createdAt"`
	LastActivityAt  int64  `json:"lastActivityAt"`
	MessageCount    int    `json:"messageCount"`
	Status          string `json:"status"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
}

type Attachment struct {
	Type        string // "image", "document", "text-file"
	Base64      string
	MimeType    string
	FileName    string
	TextContent string
}

type MessageResult struct {
	Text         string
	CreatedFiles []string
}

type SessionManager struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	procs           map[string]context.CancelFunc
	config          *Config
	stateDir        string
	memory          *MemoryManager
	resetLocation   *time.Location
	claudeCredential *syscall.Credential // nil = run as current user
}

func NewSessionManager(cfg *Config, memory *MemoryManager, cred *syscall.Credential) *SessionManager {
	home, _ := os.UserHomeDir()
	paiDir := os.Getenv("PAI_DIR")
	if paiDir == "" {
		paiDir = filepath.Join(home, ".claude")
	}
	stateDir := filepath.Join(paiDir, "skills/TelegramBridge/state")

	loc, err := time.LoadLocation(cfg.Sessions.Timezone)
	if err != nil {
		log.Printf("[PAI Bridge] Invalid timezone %q, falling back to UTC: %v", cfg.Sessions.Timezone, err)
		loc = time.UTC
	}

	sm := &SessionManager{
		sessions:         make(map[string]*Session),
		procs:            make(map[string]context.CancelFunc),
		config:           cfg,
		stateDir:         stateDir,
		memory:           memory,
		resetLocation:    loc,
		claudeCredential: cred,
	}
	sm.loadFromDisk()
	return sm
}

func (sm *SessionManager) loadFromDisk() {
	path := filepath.Join(sm.stateDir, "sessions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var sessions []*Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return
	}

	for _, s := range sessions {
		s.Status = "active"
		sm.sessions[s.UserID] = s
	}
	log.Printf("[PAI Bridge] Loaded %d session(s) from disk.", len(sessions))
}

func (sm *SessionManager) saveToDisk() {
	os.MkdirAll(sm.stateDir, 0700)
	path := filepath.Join(sm.stateDir, "sessions.json")

	var sessions []*Session
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		log.Printf("[PAI Bridge] Failed to marshal sessions: %v", err)
		return
	}
	os.WriteFile(path, data, 0600)
}

func (sm *SessionManager) CanCreate() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	active := 0
	for _, s := range sm.sessions {
		if s.Status != "idle" {
			active++
		}
	}
	return active < sm.config.Sessions.MaxConcurrent
}

func (sm *SessionManager) GetSession(userID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[userID]
}

func (sm *SessionManager) CreateSession(userID, chatID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := &Session{
		ID:             uuid.New().String(),
		UserID:         userID,
		ChatID:         chatID,
		WorkDir:        sm.config.Sessions.DefaultWorkDir,
		Model:          sm.config.Sessions.DefaultModel,
		CreatedAt:      time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(),
		MessageCount:   0,
		Status:         "active",
	}
	sm.sessions[userID] = s
	sm.saveToDisk()
	return s
}

func (sm *SessionManager) KillSession(userID string) bool {
	sm.mu.Lock()

	s, ok := sm.sessions[userID]
	if !ok {
		sm.mu.Unlock()
		return false
	}

	if cancel, exists := sm.procs[s.ID]; exists {
		cancel()
		delete(sm.procs, s.ID)
	}

	sessionID := s.ID
	model := s.Model
	msgCount := s.MessageCount

	delete(sm.sessions, userID)
	sm.saveToDisk()
	sm.mu.Unlock()

	// Flush synchronously so summary is on disk before user sends next message
	if msgCount > 0 {
		sm.memory.FlushSession(userID, sessionID, model)
	}

	return true
}

func (sm *SessionManager) ListSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var list []*Session
	for _, s := range sm.sessions {
		list = append(list, s)
	}
	return list
}

type staleSession struct {
	userID    string
	sessionID string
	model     string
}

// FlushAll synchronously flushes all active sessions with messages.
// Called during graceful shutdown to preserve context before exit.
func (sm *SessionManager) FlushAll() {
	sm.mu.RLock()
	var toFlush []staleSession
	for userID, s := range sm.sessions {
		if s.MessageCount > 0 {
			toFlush = append(toFlush, staleSession{
				userID:    userID,
				sessionID: s.ID,
				model:     s.Model,
			})
		}
	}
	sm.mu.RUnlock()

	if len(toFlush) == 0 {
		return
	}

	log.Printf("[PAI Bridge] Flushing %d session(s) before shutdown...", len(toFlush))
	for _, sf := range toFlush {
		sm.memory.FlushSession(sf.userID, sf.sessionID, sf.model)
	}
	log.Printf("[PAI Bridge] Shutdown flush complete")
}

func (sm *SessionManager) CleanStale() int {
	sm.mu.Lock()

	timeout := int64(sm.config.Sessions.TimeoutMinutes) * 60_000
	now := time.Now().UnixMilli()
	cleaned := 0

	// Daily reset check: if current hour (in configured timezone) matches reset_hour and session is idle 5+ min
	resetHour := sm.config.Sessions.ResetHour
	dailyResetActive := false
	if resetHour >= 0 {
		currentHour := time.Now().In(sm.resetLocation).Hour()
		dailyResetActive = currentHour == resetHour
	}

	var toFlush []staleSession

	for userID, s := range sm.sessions {
		if s.Status == "busy" {
			continue
		}

		idleMs := now - s.LastActivityAt
		shouldClean := false

		if idleMs > timeout {
			// Standard idle timeout
			shouldClean = true
		} else if dailyResetActive && idleMs > 5*60_000 {
			// Daily reset: clean if idle 5+ min during reset hour
			shouldClean = true
			log.Printf("[PAI Bridge] Daily reset (hour=%d) cleaning session %s", resetHour, s.ID[:8])
		}

		if shouldClean {
			if cancel, exists := sm.procs[s.ID]; exists {
				cancel()
				delete(sm.procs, s.ID)
			}
			// Collect session info for flush before deleting
			if s.MessageCount > 0 {
				toFlush = append(toFlush, staleSession{
					userID:    userID,
					sessionID: s.ID,
					model:     s.Model,
				})
			}
			delete(sm.sessions, userID)
			cleaned++
		}
	}

	if cleaned > 0 {
		sm.saveToDisk()
	}
	sm.mu.Unlock()

	// Flush sessions asynchronously (outside the lock)
	for _, sf := range toFlush {
		go sm.memory.FlushSession(sf.userID, sf.sessionID, sf.model)
	}

	// Run retention cleanup once per day during the reset window
	if dailyResetActive {
		go sm.memory.CleanOldFiles()
	}

	return cleaned
}

const bridgeContext = `[TELEGRAM BRIDGE CONTEXT]
You are responding through a Telegram chat bridge. The user is on their phone.
- Keep responses concise and mobile-friendly.
- When the user asks you to send, fetch, grab, pull, or share a FILE, output its absolute path on its own line as: SEND: /absolute/path/to/file.ext
- You can output multiple SEND: lines for multiple files.
- The bridge will automatically deliver SEND: files to the user's Telegram chat.
- Use SEND: only when the user wants to RECEIVE a file, not when you're just reading files for your own understanding.
- To speak a response as a voice note, use either format on its own line:
  VOICE: Text to be spoken aloud
  🗣️ PAI: Text to be spoken aloud
- Both forms trigger TTS synthesis. The 🗣️ PAI: form is used by the PAI Algorithm.
- Only one voice line per response. Keep voice text concise (1-3 sentences).
- The bridge will synthesize speech and deliver it as a Telegram voice message.
- For Obsidian notes: wiki-links like [[filename]] and ![[attachment]] resolve relative to the vault root. Follow links to find referenced files.
[END BRIDGE CONTEXT]

`

func (sm *SessionManager) SendMessage(userID string, text string, attachment *Attachment) (*MessageResult, error) {
	sm.mu.Lock()
	session, ok := sm.sessions[userID]
	if ok && session.Status == "busy" {
		sm.mu.Unlock()
		return nil, fmt.Errorf("Still processing your previous message. Please wait.")
	}
	if !ok {
		// Enforce concurrency limit before creating a new session
		active := 0
		for _, s := range sm.sessions {
			if s.Status != "idle" {
				active++
			}
		}
		if active >= sm.config.Sessions.MaxConcurrent {
			sm.mu.Unlock()
			return nil, fmt.Errorf("Max concurrent sessions reached. Use /clear to end your session first.")
		}
		session = &Session{
			ID:             uuid.New().String(),
			UserID:         userID,
			ChatID:         userID,
			WorkDir:        sm.config.Sessions.DefaultWorkDir,
			Model:          sm.config.Sessions.DefaultModel,
			CreatedAt:      time.Now().UnixMilli(),
			LastActivityAt: time.Now().UnixMilli(),
			MessageCount:   0,
			Status:         "active",
		}
		sm.sessions[userID] = session
	}

	session.Status = "busy"
	session.LastActivityAt = time.Now().UnixMilli()
	session.MessageCount++

	// Copy session fields under lock to avoid data race after unlock
	sessionID := session.ID
	sessionWorkDir := session.WorkDir
	sessionModel := session.Model
	claudeSessionID := session.ClaudeSessionID
	sm.mu.Unlock()

	// Resolve claude binary
	claudePath := os.Getenv("CLAUDE_PATH")
	if claudePath == "" {
		home, _ := os.UserHomeDir()
		claudePath = filepath.Join(home, ".local/bin/claude")
	}
	if resolved, err := filepath.EvalSymlinks(claudePath); err == nil {
		claudePath = resolved
	}

	// Prepend bridge context + previous session summaries + daily notes on first message
	isFirst := claudeSessionID == ""
	messageText := text
	if isFirst {
		recentContext := sm.memory.GetRecentContext(userID, sm.config.Memory.MaxSummaries)
		dailyNotes := sm.memory.GetDailyNotes(userID)
		messageText = bridgeContext + recentContext + dailyNotes + text
	}

	// Inline text-file attachments
	if attachment != nil && attachment.Type == "text-file" && attachment.TextContent != "" {
		label := attachment.FileName
		if label == "" {
			label = "document"
		}
		messageText = fmt.Sprintf("%s\n\n--- %s ---\n%s\n--- end ---", messageText, label, attachment.TextContent)
	}

	// Determine if we need stream-json input (for binary attachments)
	useStreamJSON := attachment != nil && attachment.Type != "text-file"
	hasResume := claudeSessionID != ""

	args := []string{"-p"}

	if useStreamJSON {
		args = append(args, "--input-format", "stream-json", "--output-format", "stream-json", "--verbose")
	} else {
		args = append(args, messageText, "--output-format", "stream-json", "--verbose")
	}

	args = append(args, "--model", sessionModel)

	if hasResume {
		args = append(args, "--resume", claudeSessionID)
	}

	// Log the user's message
	sm.memory.LogTurn(userID, sessionID, "user", text)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = sessionWorkDir

	// Build minimal environment for Claude subprocess — only pass what's needed.
	// This avoids leaking bridge secrets (TELEGRAM_BOT_TOKEN, CLAUDE_CODE_OAUTH_TOKEN)
	// into the subprocess.
	env := buildClaudeEnv(sm.claudeCredential != nil)
	if sm.claudeCredential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: sm.claudeCredential,
		}
	}
	cmd.Env = env

	if useStreamJSON {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("stdin pipe: %w", err)
		}

		go func() {
			defer stdin.Close()
			var content []interface{}

			if attachment.Type == "image" {
				content = append(content, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": attachment.MimeType,
						"data":       attachment.Base64,
					},
				})
			} else if attachment.Type == "document" {
				content = append(content, map[string]interface{}{
					"type": "document",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": attachment.MimeType,
						"data":       attachment.Base64,
					},
				})
			}

			defaultPrompt := messageText
			if defaultPrompt == "" {
				if attachment.Type == "image" {
					defaultPrompt = "What is in this image?"
				} else {
					defaultPrompt = "Please analyze this document."
				}
			}
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": defaultPrompt,
			})

			msg := map[string]interface{}{
				"type": "user",
				"message": map[string]interface{}{
					"role":    "user",
					"content": content,
				},
			}

			data, _ := json.Marshal(msg)
			stdin.Write(data)
			stdin.Write([]byte("\n"))
		}()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	// Register process for cancellation
	sm.mu.Lock()
	sm.procs[sessionID] = cancel
	sm.mu.Unlock()

	var fullResponse strings.Builder
	var createdFiles []string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Capture session ID
		if event["type"] == "system" {
			if sid, ok := event["session_id"].(string); ok && sid != "" && claudeSessionID == "" {
				sm.mu.Lock()
				if s, ok := sm.sessions[userID]; ok {
					s.ClaudeSessionID = sid
				}
				claudeSessionID = sid
				sm.saveToDisk()
				sm.mu.Unlock()
			}
		}

		// Extract text
		if chunk := extractTextFromEvent(event); chunk != "" {
			fullResponse.WriteString(chunk)
		}

		// Extract created files
		for _, f := range extractCreatedFilesFromEvent(event) {
			createdFiles = appendUnique(createdFiles, f)
		}
	}

	// Read stderr
	stderrScanner := bufio.NewScanner(stderr)
	var stderrBuf strings.Builder
	for stderrScanner.Scan() {
		stderrBuf.WriteString(stderrScanner.Text())
		stderrBuf.WriteString("\n")
	}

	exitErr := cmd.Wait()

	// Cleanup
	sm.mu.Lock()
	delete(sm.procs, sessionID)
	if s, ok := sm.sessions[userID]; ok {
		s.Status = "active"
	}
	sm.saveToDisk()
	sm.mu.Unlock()

	cancel() // release context

	if exitErr != nil {
		stderrText := stderrBuf.String()
		if hasResume && strings.Contains(stderrText, "Could not find session") {
			sm.mu.Lock()
			if s, ok := sm.sessions[userID]; ok {
				s.ClaudeSessionID = ""
			}
			sm.saveToDisk()
			sm.mu.Unlock()
			return nil, fmt.Errorf("Session expired. Send your message again to start a new conversation.")
		}
		if stderrText != "" {
			return nil, fmt.Errorf("Claude exited: %s", strings.TrimSpace(stderrText))
		}
	}

	// Log the assistant's response
	if responseText := fullResponse.String(); responseText != "" {
		sm.memory.LogTurn(userID, sessionID, "assistant", responseText)
	}

	return &MessageResult{
		Text:         fullResponse.String(),
		CreatedFiles: createdFiles,
	}, nil
}

func extractTextFromEvent(event map[string]interface{}) string {
	if event["type"] != "assistant" {
		return ""
	}

	msg, ok := event["message"].(map[string]interface{})
	if !ok {
		return ""
	}

	content, ok := msg["content"].([]interface{})
	if !ok {
		return ""
	}

	var sb strings.Builder
	for _, block := range content {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		if b["type"] == "text" {
			if text, ok := b["text"].(string); ok {
				sb.WriteString(text)
			}
		}
	}
	return sb.String()
}

var (
	redirectPattern   = regexp.MustCompile(`>\s*(/\S+\.\w+)`)
	outputFlagPattern = regexp.MustCompile(`(?:-o|--output)\s+["']?(\S+\.\w+)["']?`)
)

func extractCreatedFilesFromEvent(event map[string]interface{}) []string {
	if event["type"] != "assistant" {
		return nil
	}

	msg, ok := event["message"].(map[string]interface{})
	if !ok {
		return nil
	}

	content, ok := msg["content"].([]interface{})
	if !ok {
		return nil
	}

	var files []string
	for _, block := range content {
		b, ok := block.(map[string]interface{})
		if !ok || b["type"] != "tool_use" {
			continue
		}

		input, ok := b["input"].(map[string]interface{})
		if !ok {
			continue
		}

		if b["name"] == "Write" {
			if fp, ok := input["file_path"].(string); ok {
				files = append(files, fp)
			}
		}

		if b["name"] == "Bash" {
			if cmd, ok := input["command"].(string); ok {
				for _, m := range redirectPattern.FindAllStringSubmatch(cmd, -1) {
					files = append(files, m[1])
				}
				for _, m := range outputFlagPattern.FindAllStringSubmatch(cmd, -1) {
					files = append(files, m[1])
				}
			}
		}
	}

	return files
}

// claudeEnvAllowlist lists environment variable prefixes/names that Claude
// subprocesses need. Everything else (TELEGRAM_BOT_TOKEN, CLAUDE_CODE_OAUTH_TOKEN,
// ELEVENLABS_API_KEY, etc.) is deliberately excluded.
var claudeEnvAllowlist = []string{
	"PATH=",
	"LANG=",
	"LC_",
	"TERM=",
	"SHELL=",
	"USER=",
	"LOGNAME=",
	"XDG_",
	"PAI_DIR=",
	"CLAUDE_PATH=",
	"CLAUDE_USER_HOME=",
	"CLAUDE_RUN_AS_USER=",
	"CLAUDE_CODE_OAUTH_TOKEN=",
	"CLAUDE_CODE_EXPERIMENTAL_",
	"GH_TOKEN=",
	"DO_TOKEN=",
	"GOOGLE_API_KEY=",
	"GOOGLE_APPLICATION_CREDENTIALS=",
}

// buildClaudeEnv creates a filtered environment for Claude subprocesses,
// including only allowlisted variables. When running as unprivileged user,
// HOME is overridden to the pai user's home directory.
func buildClaudeEnv(asUnprivilegedUser bool) []string {
	var env []string
	for _, e := range os.Environ() {
		for _, prefix := range claudeEnvAllowlist {
			if strings.HasPrefix(e, prefix) {
				env = append(env, e)
				break
			}
		}
	}

	if asUnprivilegedUser {
		claudeHome := os.Getenv("CLAUDE_USER_HOME")
		if claudeHome == "" {
			claudeHome = "/home/pai"
		}
		env = append(env, "HOME="+claudeHome)
	} else {
		// Preserve HOME from parent if running as same user
		if home := os.Getenv("HOME"); home != "" {
			env = append(env, "HOME="+home)
		}
	}

	return env
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
