package main

import (
	"encoding/json"
	"testing"
)

func TestJsonBool(t *testing.T) {
	raw := map[string]json.RawMessage{
		"enabled": json.RawMessage(`true`),
		"broken":  json.RawMessage(`"not a bool"`),
	}

	if !jsonBool(raw, "enabled", false) {
		t.Error("should return true for true value")
	}
	if jsonBool(raw, "missing", true) != true {
		t.Error("should return default for missing key")
	}
	if jsonBool(raw, "broken", false) != false {
		t.Error("should return default for invalid bool")
	}
}

func TestJsonStringSlice(t *testing.T) {
	raw := map[string]json.RawMessage{
		"users": json.RawMessage(`["alice", "bob"]`),
		"empty": json.RawMessage(`[]`),
	}

	users := jsonStringSlice(raw, "users")
	if len(users) != 2 || users[0] != "alice" || users[1] != "bob" {
		t.Errorf("got %v", users)
	}

	empty := jsonStringSlice(raw, "empty")
	if len(empty) != 0 {
		t.Errorf("empty: got %v", empty)
	}

	missing := jsonStringSlice(raw, "missing")
	if missing != nil {
		t.Errorf("missing: got %v", missing)
	}
}

func TestJsonIntNested(t *testing.T) {
	raw := map[string]json.RawMessage{
		"sessions": json.RawMessage(`{"timeout_minutes": 120, "max_concurrent": 3}`),
	}

	if got := jsonIntNested(raw, "sessions", "timeout_minutes", 240); got != 120 {
		t.Errorf("timeout: got %d, want 120", got)
	}
	if got := jsonIntNested(raw, "sessions", "max_concurrent", 2); got != 3 {
		t.Errorf("max_concurrent: got %d, want 3", got)
	}
	if got := jsonIntNested(raw, "sessions", "missing_key", 99); got != 99 {
		t.Errorf("missing key: got %d, want 99", got)
	}
	if got := jsonIntNested(raw, "missing_section", "key", 42); got != 42 {
		t.Errorf("missing section: got %d, want 42", got)
	}
}

func TestJsonStringNested(t *testing.T) {
	raw := map[string]json.RawMessage{
		"voice": json.RawMessage(`{"voice_id": "abc123", "model": "eleven_v2"}`),
	}

	if got := jsonStringNested(raw, "voice", "voice_id", "default"); got != "abc123" {
		t.Errorf("voice_id: got %q", got)
	}
	if got := jsonStringNested(raw, "voice", "missing", "fallback"); got != "fallback" {
		t.Errorf("missing: got %q", got)
	}
	if got := jsonStringNested(raw, "nosection", "key", "def"); got != "def" {
		t.Errorf("no section: got %q", got)
	}
}

func TestJsonBoolNested(t *testing.T) {
	raw := map[string]json.RawMessage{
		"voice": json.RawMessage(`{"enabled": true}`),
	}

	if !jsonBoolNested(raw, "voice", "enabled", false) {
		t.Error("should return true")
	}
	if jsonBoolNested(raw, "voice", "missing", false) {
		t.Error("missing key should return default false")
	}
	if !jsonBoolNested(raw, "nosection", "key", true) {
		t.Error("missing section should return default true")
	}
}

func TestResolveHome(t *testing.T) {
	// Non-home path should pass through unchanged
	got := resolveHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("absolute path: got %q", got)
	}

	// Relative non-home path
	got = resolveHome("relative/path")
	if got != "relative/path" {
		t.Errorf("relative path: got %q", got)
	}

	// Home path should be expanded (we can't test exact value but it shouldn't start with ~)
	got = resolveHome("~/projects")
	if got == "~/projects" {
		t.Error("home path should be expanded")
	}
}
