package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("[PAI Bridge] Failed to load config: %v", err)
	}

	if !cfg.Enabled {
		log.Println("[PAI Bridge] Disabled in settings.json (telegramBridge.enabled = false). Exiting.")
		os.Exit(0)
	}

	// Look up unprivileged user for Claude subprocess isolation
	var claudeCredential *syscall.Credential
	claudeUser := os.Getenv("CLAUDE_RUN_AS_USER")
	if claudeUser == "" {
		claudeUser = "pai"
	}
	if u, err := user.Lookup(claudeUser); err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		claudeCredential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		log.Printf("[PAI Bridge] Claude subprocesses will run as user %s (uid=%d, gid=%d)", claudeUser, uid, gid)
	} else {
		log.Printf("[PAI Bridge] WARNING: user %q not found, Claude will run as current user: %v", claudeUser, err)
	}

	// Memory manager
	memory := NewMemoryManager(cfg)
	log.Printf("[PAI Bridge] Memory logging enabled=%v, path=%s", cfg.Memory.Enabled, cfg.Memory.BasePath)

	// Session manager
	sessions := NewSessionManager(cfg, memory, claudeCredential)

	// Telegram bot
	bot, err := NewBot(cfg, sessions)
	if err != nil {
		log.Fatalf("[PAI Bridge] Failed to create bot: %v", err)
	}

	// Health check server
	mux := http.NewServeMux()
	startTime := time.Now()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pollAgo := bot.LastPollSecondsAgo()
		status := "ok"
		if pollAgo < 0 || pollAgo > 120 {
			status = "degraded"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":              status,
			"service":             "pai-telegram-bridge",
			"uptime":              time.Since(startTime).Seconds(),
			"last_poll_seconds_ago": pollAgo,
			"timestamp":           time.Now().UTC().Format(time.RFC3339),
		})
	})

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		log.Printf("[PAI Bridge] Health server listening on http://localhost%s/health", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[PAI Bridge] Health server error: %v", err)
		}
	}()

	// Stale session cleanup + rate limiter pruning
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sessions.CleanStale()
			bot.cleanRateMap()
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("[PAI Bridge] Shutting down...")
		bot.Stop()
		sessions.FlushAll()
		os.Exit(0)
	}()

	// Start bot (blocking)
	log.Println("[PAI Bridge] Starting bot with long-polling...")
	bot.Start()
}
