package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var imageExtRe = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)$`)


type Bot struct {
	api        *tgbotapi.BotAPI
	config     *Config
	sessions   *SessionManager
	rateMap    map[string][]int64
	rateMu     sync.Mutex
	lastPollAt atomic.Int64 // unix milli of last successful poll cycle
	stopCh     chan struct{}
}

func NewBot(cfg *Config, sessions *SessionManager) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}

	return &Bot{
		api:      api,
		config:   cfg,
		sessions: sessions,
		rateMap:  make(map[string][]int64),
		stopCh:   make(chan struct{}),
	}, nil
}

func (b *Bot) Start() {
	// Register commands
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Show bridge info"},
		{Command: "status", Description: "Current session status"},
		{Command: "clear", Description: "End current session"},
		{Command: "cd", Description: "Change working directory"},
		{Command: "sessions", Description: "List active sessions"},
	}
	cmdCfg := tgbotapi.NewSetMyCommands(commands...)
	b.api.Request(cmdCfg)

	log.Println("[PAI Bridge] Bot is running.")

	var offset int
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		u := tgbotapi.NewUpdate(offset)
		u.Timeout = 60
		updates, err := b.api.GetUpdates(u)
		b.lastPollAt.Store(time.Now().UnixMilli())

		if err != nil {
			log.Printf("[PAI Bridge] Poll error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			offset = update.UpdateID + 1
			if update.Message == nil {
				continue
			}
			go b.handleUpdate(update)
		}
	}
}

func (b *Bot) Stop() {
	close(b.stopCh)
}

// LastPollSecondsAgo returns how many seconds since the last successful poll cycle.
func (b *Bot) LastPollSecondsAgo() float64 {
	last := b.lastPollAt.Load()
	if last == 0 {
		return -1
	}
	return float64(time.Now().UnixMilli()-last) / 1000.0
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	msg := update.Message
	userID := fmt.Sprintf("%d", msg.From.ID)

	if !b.authorize(msg) {
		return
	}

	// Handle commands
	if msg.IsCommand() {
		b.handleCommand(msg, userID)
		return
	}

	// Handle messages with attachments
	if msg.Photo != nil && len(msg.Photo) > 0 {
		b.handlePhoto(msg, userID)
		return
	}

	if msg.Document != nil {
		b.handleDocument(msg, userID)
		return
	}

	// Plain text
	if msg.Text != "" {
		b.handleMessage(msg.Chat.ID, userID, msg.Text, nil)
	}
}

func (b *Bot) handleCommand(msg *tgbotapi.Message, userID string) {
	chatID := msg.Chat.ID

	switch msg.Command() {
	case "start":
		text := fmt.Sprintf("PAI Telegram Bridge active.\n\nYour user ID: %s\nModel: %s\nWork dir: %s\n\nSend any message to start a conversation with PAI.",
			userID, b.config.Sessions.DefaultModel, b.config.Sessions.DefaultWorkDir)
		b.send(chatID, text)

	case "status":
		session := b.sessions.GetSession(userID)
		if session == nil {
			b.send(chatID, "No active session. Send a message to start one.")
			return
		}
		text := fmt.Sprintf("Session: %s...\nStatus: %s\nMessages: %d\nModel: %s\nWork dir: %s\nStarted: %s",
			session.ID[:8], session.Status, session.MessageCount, session.Model, session.WorkDir,
			time.UnixMilli(session.CreatedAt).Format(time.RFC822))
		b.send(chatID, text)

	case "clear":
		killed := b.sessions.KillSession(userID)
		if killed {
			b.send(chatID, "Session cleared.")
		} else {
			b.send(chatID, "No active session.")
		}

	case "cd":
		dir := strings.TrimSpace(msg.CommandArguments())
		if dir == "" {
			b.send(chatID, fmt.Sprintf("Current work dir: %s\n\nUsage: /cd /path/to/project", b.config.Sessions.DefaultWorkDir))
			return
		}
		if strings.HasPrefix(dir, "~/") {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, dir[2:])
		}
		// Resolve symlinks for comparison (~/projects -> /mnt/pai-data/projects)
		resolvedDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolvedDir = dir
		}
		resolvedDefault, err := filepath.EvalSymlinks(b.config.Sessions.DefaultWorkDir)
		if err != nil {
			resolvedDefault = b.config.Sessions.DefaultWorkDir
		}
		if !strings.HasPrefix(resolvedDir, resolvedDefault) && !strings.HasPrefix(resolvedDir, "/mnt/pai-data") {
			b.send(chatID, fmt.Sprintf("Path not allowed. Must be under %s or /mnt/pai-data.", b.config.Sessions.DefaultWorkDir))
			return
		}
		session := b.sessions.GetSession(userID)
		if session == nil {
			session = b.sessions.CreateSession(userID, fmt.Sprintf("%d", chatID))
		}
		b.sessions.SetWorkDir(userID, dir)
		b.send(chatID, fmt.Sprintf("Work directory set to: %s", dir))

	case "sessions":
		list := b.sessions.ListSessions()
		if len(list) == 0 {
			b.send(chatID, "No active sessions.")
			return
		}
		var lines []string
		for _, s := range list {
			lines = append(lines, fmt.Sprintf("%s... | %s | %d msgs | %s", s.ID[:8], s.Status, s.MessageCount, s.WorkDir))
		}
		b.send(chatID, "Active sessions:\n\n"+strings.Join(lines, "\n"))
	}
}

func (b *Bot) handlePhoto(msg *tgbotapi.Message, userID string) {
	photos := msg.Photo
	largest := photos[len(photos)-1]

	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: largest.FileID})
	if err != nil {
		b.send(msg.Chat.ID, fmt.Sprintf("Error getting photo: %v", err))
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.config.BotToken, file.FilePath)
	data, err := downloadFile(url)
	if err != nil {
		b.send(msg.Chat.ID, fmt.Sprintf("Error downloading photo: %v", err))
		return
	}

	ext := filepath.Ext(file.FilePath)
	mimeType := "image/jpeg"
	switch strings.ToLower(ext) {
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	}

	attachment := &Attachment{
		Type:     "image",
		Base64:   base64.StdEncoding.EncodeToString(data),
		MimeType: mimeType,
	}

	caption := msg.Caption
	if caption == "" {
		caption = ""
	}

	b.handleMessage(msg.Chat.ID, userID, caption, attachment)
}

func (b *Bot) handleDocument(msg *tgbotapi.Message, userID string) {
	doc := msg.Document
	fileName := doc.FileName
	if fileName == "" {
		fileName = "document"
	}

	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: doc.FileID})
	if err != nil {
		b.send(msg.Chat.ID, fmt.Sprintf("Error getting document: %v", err))
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.config.BotToken, file.FilePath)
	data, err := downloadFile(url)
	if err != nil {
		b.send(msg.Chat.ID, fmt.Sprintf("Error downloading document: %v", err))
		return
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	if len(ext) > 0 {
		ext = ext[1:] // remove leading dot
	}

	var attachment *Attachment

	if ext == "pdf" {
		attachment = &Attachment{
			Type:     "document",
			Base64:   base64.StdEncoding.EncodeToString(data),
			MimeType: "application/pdf",
			FileName: fileName,
		}
	} else if isTextExt(ext) {
		attachment = &Attachment{
			Type:        "text-file",
			MimeType:    "text/plain",
			FileName:    fileName,
			TextContent: string(data),
		}
	} else {
		b.send(msg.Chat.ID, fmt.Sprintf("Unsupported file type: .%s. I can handle PDF, text, code, and data files.", ext))
		return
	}

	caption := msg.Caption
	if caption == "" {
		caption = ""
	}

	b.handleMessage(msg.Chat.ID, userID, caption, attachment)
}

func (b *Bot) handleMessage(chatID int64, userID, text string, attachment *Attachment) {
	if b.isRateLimited(userID) {
		b.send(chatID, "Rate limited. Please wait a moment.")
		return
	}

	session := b.sessions.GetSession(userID)
	if session == nil && !b.sessions.CanCreate() {
		b.send(chatID, "Max concurrent sessions reached. Use /clear to end your session first.")
		return
	}

	// Send typing indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	b.api.Send(typing)

	// Keep typing indicator alive
	stopTyping := make(chan struct{})
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopTyping:
				return
			case <-ticker.C:
				b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
			}
		}
	}()

	result, err := b.sessions.SendMessage(userID, text, attachment)
	close(stopTyping)

	if err != nil {
		b.send(chatID, fmt.Sprintf("Error: %v", err))
		return
	}

	if strings.TrimSpace(result.Text) == "" {
		b.send(chatID, "(No response from Claude)")
		return
	}

	// Extract SEND: directives
	cleanText, sendPaths := extractSendDirectives(result.Text)

	// Parse and format response
	chunks := parseResponse(cleanText, b.config.Response.Format)

	for _, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Send(msg); err != nil {
			// Fallback to plain text
			log.Printf("[PAI Bridge] HTML parse failed, falling back: %v", err)
			msg.ParseMode = ""
			b.api.Send(msg)
		}
	}

	// Only deliver files explicitly requested via SEND: directives
	seen := make(map[string]bool)
	var allFiles []string
	for _, p := range sendPaths {
		norm, _ := filepath.Abs(p)
		if !seen[norm] {
			seen[norm] = true
			allFiles = append(allFiles, norm)
		}
	}

	// Send files
	for _, fp := range allFiles {
		if _, err := os.Stat(fp); os.IsNotExist(err) {
			continue
		}
		if imageExtRe.MatchString(fp) {
			photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(fp))
			if _, err := b.api.Send(photo); err != nil {
				log.Printf("[PAI Bridge] Failed to send photo %s: %v", fp, err)
			}
		} else {
			doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(fp))
			if _, err := b.api.Send(doc); err != nil {
				log.Printf("[PAI Bridge] Failed to send document %s: %v", fp, err)
			}
		}
	}
}

// --- Auth & Rate Limiting ---

func (b *Bot) authorize(msg *tgbotapi.Message) bool {
	if msg.From == nil {
		return false
	}
	userID := fmt.Sprintf("%d", msg.From.ID)

	if len(b.config.AllowedUsers) == 0 {
		return true
	}

	for _, allowed := range b.config.AllowedUsers {
		if allowed == userID {
			return true
		}
	}

	b.send(msg.Chat.ID, "Unauthorized. Your user ID is not in the allowlist.")
	return false
}

func (b *Bot) isRateLimited(userID string) bool {
	b.rateMu.Lock()
	defer b.rateMu.Unlock()

	now := time.Now().UnixMilli()
	window := int64(60_000)

	timestamps := b.rateMap[userID]
	var recent []int64
	for _, t := range timestamps {
		if now-t < window {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	b.rateMap[userID] = recent

	return len(recent) > b.config.Security.RateLimitPerMinute
}

// cleanRateMap removes stale entries from the rate limiter map.
func (b *Bot) cleanRateMap() {
	b.rateMu.Lock()
	defer b.rateMu.Unlock()

	now := time.Now().UnixMilli()
	window := int64(60_000)
	for userID, timestamps := range b.rateMap {
		var recent []int64
		for _, t := range timestamps {
			if now-t < window {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(b.rateMap, userID)
		} else {
			b.rateMap[userID] = recent
		}
	}
}

func (b *Bot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}

// --- Helpers ---

func extractSendDirectives(text string) (string, []string) {
	var sendPaths []string
	var cleanLines []string

	sendRe := regexp.MustCompile(`^SEND:\s*(.+)$`)
	for _, line := range strings.Split(text, "\n") {
		if match := sendRe.FindStringSubmatch(line); match != nil {
			p := strings.TrimSpace(match[1])
			if strings.HasPrefix(p, "~/") {
				home, _ := os.UserHomeDir()
				p = filepath.Join(home, p[2:])
			}
			sendPaths = append(sendPaths, p)
		} else {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n"), sendPaths
}


const maxDownloadSize = 50 * 1024 * 1024 // 50MB

var httpClient = &http.Client{Timeout: 30 * time.Second}

func downloadFile(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
}

func isTextExt(ext string) bool {
	textExts := map[string]bool{
		"txt": true, "md": true, "csv": true, "json": true, "xml": true,
		"html": true, "yml": true, "yaml": true, "toml": true, "ini": true,
		"log": true, "py": true, "js": true, "ts": true, "sh": true,
		"rb": true, "go": true, "rs": true, "java": true, "c": true,
		"cpp": true, "h": true, "css": true, "sql": true,
	}
	return textExts[ext]
}
