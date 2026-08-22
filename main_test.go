package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateNickname(t *testing.T) {
	if got, err := validateNickname("  Alex "); err != nil || got != "Alex" {
		t.Fatalf("validateNickname() = %q, %v", got, err)
	}
	if _, err := validateNickname(tooLong(maxNicknameRunes + 1)); err == nil {
		t.Fatal("expected long nickname to be rejected")
	}
	if _, err := validateNickname("   "); err == nil {
		t.Fatal("expected empty nickname to be rejected")
	}
}

func TestValidateMessage(t *testing.T) {
	if got, err := validateMessage("  hello world  "); err != nil || got != "hello world" {
		t.Fatalf("validateMessage() = %q, %v", got, err)
	}
	if _, err := validateMessage("\n\t"); err == nil {
		t.Fatal("expected empty message to be rejected")
	}
	if _, err := validateMessage(tooLong(maxMessageRunes + 1)); err == nil {
		t.Fatal("expected long message to be rejected")
	}
}

func TestMessageJSONLAndRetention(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "messages.jsonl")
	store := &messageStore{path: path, retention: 30 * 24 * time.Hour}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"timestamp":"2026-08-20T12:00:00Z","nickname":"Alex","message":"recent"}
{"timestamp":"2026-07-20T11:59:59Z","nickname":"Alex","message":"expired"}
not-json
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	history, err := store.history(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Message != "recent" || history[0].Timestamp.Location() != time.UTC {
		t.Fatalf("unexpected history: %#v", history)
	}
	if err := store.cleanup(now); err != nil {
		t.Fatal(err)
	}
	cleaned, err := store.history(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 1 || cleaned[0].Message != "recent" {
		t.Fatalf("unexpected cleaned history: %#v", cleaned)
	}
}

func TestConfiguredVAPIDSubject(t *testing.T) {
	t.Setenv("VAPID_SUBJECT", "mailto:chat@example.com")
	if got, err := configuredVAPIDSubject("mailto:backup-chat@localhost"); err != nil || got != "mailto:chat@example.com" {
		t.Fatalf("configuredVAPIDSubject() = %q, %v", got, err)
	}

	t.Setenv("VAPID_SUBJECT", "chat.example.com")
	if _, err := configuredVAPIDSubject("mailto:backup-chat@localhost"); err == nil {
		t.Fatal("expected a bare hostname to be rejected")
	}
}

func TestStaticAssetsMustRevalidate(t *testing.T) {
	request := httptest.NewRequest("GET", "/style.css", nil)
	response := httptest.NewRecorder()

	staticHandler().ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func tooLong(length int) string {
	value := make([]rune, length)
	for i := range value {
		value[i] = 'x'
	}
	return string(value)
}
