package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	defaultPort         = "50000"
	defaultDataFile    = "./data/messages.jsonl"
	defaultRetention    = 30
	maxNicknameRunes   = 32
	maxMessageRunes    = 2000
	maxWebSocketBytes  = 8192
	messageInterval    = 500 * time.Millisecond
)

//go:embed web/index.html web/app.js web/style.css
var webFiles embed.FS

type Message struct {
	Timestamp time.Time `json:"timestamp"`
	Nickname  string    `json:"nickname"`
	Message   string    `json:"message"`
}

type incomingMessage struct {
	Message string `json:"message"`
}

type messageStore struct {
	path      string
	retention time.Duration
	mu        sync.Mutex
}

func (s *messageStore) prepare() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	return s.compactLocked(time.Now().UTC())
}

func (s *messageStore) append(message Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func (s *messageStore) history(now time.Time) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readValidLocked(now)
}

func (s *messageStore) cleanup(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked(now)
}

func (s *messageStore) readValidLocked(now time.Time) ([]Message, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	cutoff := now.Add(-s.retention)
	var messages []Message
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxWebSocketBytes)
	for scanner.Scan() {
		var message Message
		if json.Unmarshal(scanner.Bytes(), &message) != nil || !validStoredMessage(message) || message.Timestamp.Before(cutoff) {
			continue
		}
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *messageStore) compactLocked(now time.Time) error {
	messages, err := s.readValidLocked(now)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".messages-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	encoder := json.NewEncoder(temporary)
	for _, message := range messages {
		if err := encoder.Encode(message); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, s.path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func validStoredMessage(message Message) bool {
	return !message.Timestamp.IsZero() && strings.TrimSpace(message.Nickname) != "" &&
		utf8.RuneCountInString(message.Nickname) <= maxNicknameRunes &&
		strings.TrimSpace(message.Message) != "" && utf8.RuneCountInString(message.Message) <= maxMessageRunes
}

func validateNickname(nickname string) (string, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || utf8.RuneCountInString(nickname) > maxNicknameRunes {
		return "", fmt.Errorf("nickname must be between 1 and %d characters", maxNicknameRunes)
	}
	return nickname, nil
}

func validateMessage(message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" || utf8.RuneCountInString(message) > maxMessageRunes {
		return "", fmt.Errorf("message must be between 1 and %d characters", maxMessageRunes)
	}
	return message, nil
}

type chatClient struct {
	connection *websocket.Conn
	writeMu    sync.Mutex
}

func (c *chatClient) send(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.connection.WriteJSON(value)
}

type chatHub struct {
	mu      sync.RWMutex
	clients map[*chatClient]struct{}
}

func newChatHub() *chatHub { return &chatHub{clients: make(map[*chatClient]struct{})} }

func (h *chatHub) add(client *chatClient) {
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
}

func (h *chatHub) remove(client *chatClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

func (h *chatHub) broadcast(message Message) {
	h.mu.RLock()
	clients := make([]*chatClient, 0, len(h.clients))
	for client := range h.clients { clients = append(clients, client) }
	h.mu.RUnlock()
	for _, client := range clients {
		if err := client.send(message); err != nil {
			log.Printf("sending message: %v", err)
		}
	}
}

func websocketHandler(store *messageStore, hub *chatHub) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" { return true }
			return strings.EqualFold(origin, "http://"+request.Host) || strings.EqualFold(origin, "https://"+request.Host)
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		nickname, err := validateNickname(r.URL.Query().Get("nickname"))
		if err != nil { http.Error(w, err.Error(), http.StatusBadRequest); return }
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil { return }
		// The HTTP server's response timeout must not expire an otherwise idle chat.
		_ = connection.UnderlyingConn().SetReadDeadline(time.Time{})
		_ = connection.UnderlyingConn().SetWriteDeadline(time.Time{})
		client := &chatClient{connection: connection}
		defer connection.Close()
		defer hub.remove(client)
		history, err := store.history(time.Now().UTC())
		if err != nil { log.Printf("loading history: %v", err); return }
		for _, message := range history {
			if err := client.send(message); err != nil { return }
		}
		hub.add(client)
		connection.SetReadLimit(maxWebSocketBytes)
		var lastMessage time.Time
		for {
			var incoming incomingMessage
			if err := readJSONMessage(connection, &incoming); err != nil { return }
			if !lastMessage.IsZero() && time.Since(lastMessage) < messageInterval {
				_ = client.send(map[string]string{"error": "please wait before sending another message"})
				continue
			}
			text, err := validateMessage(incoming.Message)
			if err != nil {
				_ = client.send(map[string]string{"error": err.Error()})
				continue
			}
			message := Message{Timestamp: time.Now().UTC(), Nickname: nickname, Message: text}
			if err := store.append(message); err != nil {
				log.Printf("saving message: %v", err)
				_ = client.send(map[string]string{"error": "message could not be saved"})
				continue
			}
			lastMessage = time.Now()
			hub.broadcast(message)
		}
	}
}

func readJSONMessage(connection *websocket.Conn, destination *incomingMessage) error {
	_, reader, err := connection.NextReader()
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(reader, maxWebSocketBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func staticHandler() http.Handler {
	content, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(content))
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" { return value }
	return fallback
}

func retentionFromEnv() time.Duration {
	days, err := strconv.Atoi(envOrDefault("RETENTION_DAYS", strconv.Itoa(defaultRetention)))
	if err != nil || days < 1 { days = defaultRetention }
	return time.Duration(days) * 24 * time.Hour
}

func main() {
	store := &messageStore{path: envOrDefault("DATA_FILE", defaultDataFile), retention: retentionFromEnv()}
	if err := store.prepare(); err != nil { log.Fatalf("preparing message store: %v", err) }

	hub := newChatHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocketHandler(store, hub))
	mux.Handle("/", staticHandler())
	server := &http.Server{
		Addr:              ":" + envOrDefault("PORT", defaultPort),
		Handler:           limitRequestBody(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    4096,
	}

	cleanupDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		defer close(cleanupDone)
		for range ticker.C {
			if err := store.cleanup(time.Now().UTC()); err != nil { log.Printf("cleaning message store: %v", err) }
		}
	}()
	go func() {
		log.Printf("listening on %s", server.Addr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) { log.Fatalf("server: %v", err) }
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil { log.Printf("shutting down server: %v", err) }
	select { case <-cleanupDone: case <-ctx.Done(): }
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil { r.Body = http.MaxBytesReader(w, r.Body, 16*1024) }
		next.ServeHTTP(w, r)
	})
}
