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
	defaultPort        = "50000"
	defaultDataFile    = "./data/messages.jsonl"
	defaultRetention   = 30
	maxNicknameRunes   = 32
	maxMessageRunes    = 2000
	maxWebSocketBytes  = 8192
	messageInterval    = 500 * time.Millisecond
	websocketPingEvery = 20 * time.Second
)

//go:embed web/index.html web/app.js web/style.css web/manifest.webmanifest web/service-worker.js web/icon.svg
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
	nickname   string
	writeMu    sync.Mutex
}

func (c *chatClient) send(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.connection.WriteJSON(value)
}

func (c *chatClient) ping() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
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
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		if err := client.send(message); err != nil {
			log.Printf("sending message: %v", err)
		}
	}
}

func websocketHandler(store *messageStore, hub *chatHub, push *pushStore) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" || origin == "null" {
				return true
			}
			return strings.EqualFold(origin, "http://"+request.Host) || strings.EqualFold(origin, "https://"+request.Host)
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		nickname, err := validateNickname(r.URL.Query().Get("nickname"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// The HTTP server's response timeout must not expire an otherwise idle chat.
		_ = connection.UnderlyingConn().SetReadDeadline(time.Time{})
		_ = connection.UnderlyingConn().SetWriteDeadline(time.Time{})
		client := &chatClient{connection: connection, nickname: nickname}
		log.Printf("websocket: connected nickname=%q", nickname)
		defer connection.Close()
		defer hub.remove(client)
		defer log.Printf("websocket: closed nickname=%q", nickname)
		connection.SetPongHandler(func(string) error {
			log.Printf("websocket: keepalive pong nickname=%q", nickname)
			return nil
		})
		keepAliveDone := make(chan struct{})
		defer close(keepAliveDone)
		go keepWebSocketAlive(client, keepAliveDone)
		history, err := store.history(time.Now().UTC())
		if err != nil {
			log.Printf("loading history: %v", err)
			return
		}
		for _, message := range history {
			if err := client.send(message); err != nil {
				log.Printf("websocket: sending history nickname=%q: %v", nickname, err)
				return
			}
		}
		log.Printf("websocket: sent %d history message(s) nickname=%q", len(history), nickname)
		hub.add(client)
		connection.SetReadLimit(maxWebSocketBytes)
		var lastMessage time.Time
		for {
			var incoming incomingMessage
			if err := readJSONMessage(connection, &incoming); err != nil {
				log.Printf("websocket: read ended nickname=%q: %v", nickname, err)
				return
			}
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
			go push.send(message)
		}
	}
}

func keepWebSocketAlive(client *chatClient, done <-chan struct{}) {
	ticker := time.NewTicker(websocketPingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := client.ping(); err != nil {
				log.Printf("websocket: keepalive ping failed nickname=%q: %v", client.nickname, err)
				_ = client.connection.Close()
				return
			}
			log.Printf("websocket: keepalive ping nickname=%q", client.nickname)
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
	files := http.FileServer(http.FS(content))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Revalidate embedded assets on each visit so a new deployment is visible
		// without requiring users to perform a hard refresh. The service worker
		// still provides these files when the network is unavailable.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func retentionFromEnv() time.Duration {
	days, err := strconv.Atoi(envOrDefault("RETENTION_DAYS", strconv.Itoa(defaultRetention)))
	if err != nil || days < 1 {
		days = defaultRetention
	}
	return time.Duration(days) * 24 * time.Hour
}

func main() {
	store := &messageStore{path: envOrDefault("DATA_FILE", defaultDataFile), retention: retentionFromEnv()}
	if err := store.prepare(); err != nil {
		log.Fatalf("preparing message store: %v", err)
	}
	push, err := newPushStore(store.path)
	if err != nil {
		log.Fatalf("preparing push notifications: %v", err)
	}

	hub := newChatHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocketHandler(store, hub, push))
	mux.HandleFunc("/push/config", pushConfigHandler(push))
	mux.HandleFunc("/push/subscribe", pushSubscribeHandler(push))
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
			if err := store.cleanup(time.Now().UTC()); err != nil {
				log.Printf("cleaning message store: %v", err)
			}
		}
	}()
	go func() {
		log.Printf("listening on %s", server.Addr)
		var err error
		certificateFile := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
		keyFile := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
		if certificateFile != "" || keyFile != "" {
			if certificateFile == "" || keyFile == "" {
				log.Fatal("TLS_CERT_FILE and TLS_KEY_FILE must be set together")
			}
			err = server.ListenAndServeTLS(certificateFile, keyFile)
		} else {
			err = server.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutting down server: %v", err)
	}
	select {
	case <-cleanupDone:
	case <-ctx.Done():
	}
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		}
		next.ServeHTTP(w, r)
	})
}
