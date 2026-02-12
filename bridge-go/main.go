package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	// Memory manager
	memory := NewMemoryManager(cfg)
	log.Printf("[PAI Bridge] Memory logging enabled=%v, path=%s", cfg.Memory.Enabled, cfg.Memory.BasePath)

	// Session manager
	sessions := NewSessionManager(cfg, memory)

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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"service":   "pai-telegram-bridge",
			"uptime":    time.Since(startTime).Seconds(),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		log.Printf("[PAI Bridge] Health server listening on http://localhost%s/health", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[PAI Bridge] Health server error: %v", err)
		}
	}()

	// Stale session cleanup
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sessions.CleanStale()
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
