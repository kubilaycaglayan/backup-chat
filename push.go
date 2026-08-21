package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	maxPushEndpointLength = 2048
	maxPushKeyLength      = 256
)

type vapidConfig struct {
	Subject    string `json:"subject"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

type storedPushSubscription struct {
	Nickname     string               `json:"nickname"`
	Subscription webpush.Subscription `json:"subscription"`
}

type pushSubscribeRequest struct {
	Nickname     string               `json:"nickname"`
	Subscription webpush.Subscription `json:"subscription"`
}

type pushStore struct {
	subscriptionsPath string
	config            vapidConfig
	mu                sync.Mutex
	subscriptions     []storedPushSubscription
}

func newPushStore(dataFile string) (*pushStore, error) {
	store := &pushStore{subscriptionsPath: dataFile + ".subscriptions.json"}
	configPath := dataFile + ".vapid.json"
	config, err := loadVAPIDConfig(configPath)
	if err != nil {
		return nil, err
	}
	store.config = config
	if err := store.loadSubscriptions(); err != nil {
		return nil, err
	}
	return store, nil
}

func loadVAPIDConfig(path string) (vapidConfig, error) {
	if publicKey := strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")); publicKey != "" {
		privateKey := strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))
		if privateKey == "" {
			return vapidConfig{}, errors.New("VAPID_PRIVATE_KEY is required with VAPID_PUBLIC_KEY")
		}
		subject, err := configuredVAPIDSubject("mailto:backup-chat@localhost")
		if err != nil {
			return vapidConfig{}, err
		}
		log.Print("push: using VAPID keys from environment")
		return vapidConfig{
			Subject:    subject,
			PublicKey:  publicKey,
			PrivateKey: privateKey,
		}, nil
	}

	data, err := os.ReadFile(path)
	if err == nil {
		var config vapidConfig
		if json.Unmarshal(data, &config) == nil && config.PublicKey != "" && config.PrivateKey != "" {
			configuredSubject, err := configuredVAPIDSubject(config.Subject)
			if err != nil {
				return vapidConfig{}, err
			}
			if configuredSubject != config.Subject {
				log.Print("push: using VAPID subject from environment")
				config.Subject = configuredSubject
			}
			log.Print("push: loaded VAPID keys from persistent storage")
			return config, nil
		}
		return vapidConfig{}, fmt.Errorf("invalid VAPID key file %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return vapidConfig{}, err
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return vapidConfig{}, fmt.Errorf("generating VAPID keys: %w", err)
	}
	subject, err := configuredVAPIDSubject("mailto:backup-chat@localhost")
	if err != nil {
		return vapidConfig{}, err
	}
	config := vapidConfig{
		Subject:    subject,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}
	if err := writeJSONAtomically(path, config, 0600); err != nil {
		return vapidConfig{}, fmt.Errorf("saving VAPID keys: %w", err)
	}
	log.Print("push: generated new VAPID keys")
	return config, nil
}

func configuredVAPIDSubject(fallback string) (string, error) {
	subject := envOrDefault("VAPID_SUBJECT", fallback)
	parsed, err := url.ParseRequestURI(subject)
	if err != nil {
		return "", fmt.Errorf("VAPID_SUBJECT must be a mailto: or https: contact URI: %w", err)
	}
	switch parsed.Scheme {
	case "mailto":
		if parsed.Opaque != "" {
			return subject, nil
		}
	case "https":
		if parsed.Host != "" {
			return subject, nil
		}
	}
	return "", errors.New("VAPID_SUBJECT must be a mailto: or https: contact URI")
}

func (s *pushStore) loadSubscriptions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.subscriptionsPath)
	if errors.Is(err, os.ErrNotExist) {
		log.Print("push: no saved subscriptions")
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.subscriptions); err != nil {
		return fmt.Errorf("reading push subscriptions: %w", err)
	}
	filtered := s.subscriptions[:0]
	for _, subscription := range s.subscriptions {
		if validPushSubscription(subscription.Subscription) {
			filtered = append(filtered, subscription)
		}
	}
	s.subscriptions = filtered
	log.Printf("push: loaded %d subscription(s)", len(s.subscriptions))
	return nil
}

func subscriptionID(subscription webpush.Subscription) string {
	digest := sha256.Sum256([]byte(subscription.Endpoint))
	return fmt.Sprintf("%x", digest[:6])
}

func validPushSubscription(subscription webpush.Subscription) bool {
	return strings.HasPrefix(subscription.Endpoint, "https://") &&
		len(subscription.Endpoint) <= maxPushEndpointLength &&
		len(subscription.Keys.P256dh) > 0 && len(subscription.Keys.P256dh) <= maxPushKeyLength &&
		len(subscription.Keys.Auth) > 0 && len(subscription.Keys.Auth) <= maxPushKeyLength
}

func (s *pushStore) add(subscription storedPushSubscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].Subscription.Endpoint == subscription.Subscription.Endpoint {
			s.subscriptions[i] = subscription
			return s.saveLocked()
		}
	}
	s.subscriptions = append(s.subscriptions, subscription)
	return s.saveLocked()
}

func (s *pushStore) remove(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.subscriptions[:0]
	for _, subscription := range s.subscriptions {
		if subscription.Subscription.Endpoint != endpoint {
			filtered = append(filtered, subscription)
		}
	}
	s.subscriptions = filtered
	return s.saveLocked()
}

func (s *pushStore) saveLocked() error {
	return writeJSONAtomically(s.subscriptionsPath, s.subscriptions, 0600)
}

func (s *pushStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subscriptions)
}

func (s *pushStore) send(message Message) {
	s.mu.Lock()
	subscriptions := append([]storedPushSubscription(nil), s.subscriptions...)
	s.mu.Unlock()
	if len(subscriptions) == 0 {
		log.Printf("push: no subscriptions available for message from %q", message.Nickname)
		return
	}
	log.Printf("push: delivering notification for message from %q to %d subscription(s)", message.Nickname, len(subscriptions))
	payload, err := json.Marshal(map[string]string{
		"title": "Backup Chat",
		"body":  message.Nickname + ": " + message.Message,
		"url":   "/",
	})
	if err != nil {
		log.Printf("encoding push notification: %v", err)
		return
	}
	for _, subscription := range subscriptions {
		id := subscriptionID(subscription.Subscription)
		if subscription.Nickname == message.Nickname {
			log.Printf("push: skipping sender subscription id=%s", id)
			continue
		}
		log.Printf("push: sending notification recipient=%q id=%s", subscription.Nickname, id)
		response, err := webpush.SendNotification(payload, &subscription.Subscription, &webpush.Options{
			Subscriber:      s.config.Subject,
			VAPIDPublicKey:  s.config.PublicKey,
			VAPIDPrivateKey: s.config.PrivateKey,
			TTL:             60,
		})
		if err != nil {
			log.Printf("push: delivery failed recipient=%q id=%s: %v", subscription.Nickname, id, err)
			continue
		}
		status := response.StatusCode
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		_ = response.Body.Close()
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			log.Printf("push: delivery accepted recipient=%q id=%s status=%d", subscription.Nickname, id, status)
		} else {
			log.Printf("push: delivery rejected recipient=%q id=%s status=%d body=%q", subscription.Nickname, id, status, responseBody)
		}
		if status == http.StatusGone || status == http.StatusNotFound {
			if err := s.remove(subscription.Subscription.Endpoint); err != nil {
				log.Printf("push: removing expired subscription id=%s: %v", id, err)
			} else {
				log.Printf("push: removed expired subscription id=%s", id)
			}
		}
	}
}

func writeJSONAtomically(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".push-*.tmp")
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
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := json.NewEncoder(temporary).Encode(value); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func pushConfigHandler(push *pushStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Print("push: configuration requested")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"publicKey": push.config.PublicKey})
	}
}

func pushSubscribeHandler(push *pushStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Printf("push: rejected subscription request method=%s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request pushSubscribeRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			log.Printf("push: rejected malformed subscription request: %v", err)
			http.Error(w, "invalid subscription", http.StatusBadRequest)
			return
		}
		nickname, err := validateNickname(request.Nickname)
		if err != nil || !validPushSubscription(request.Subscription) {
			log.Printf("push: rejected invalid subscription nickname=%q", request.Nickname)
			http.Error(w, "invalid subscription", http.StatusBadRequest)
			return
		}
		if err := push.add(storedPushSubscription{Nickname: nickname, Subscription: request.Subscription}); err != nil {
			log.Printf("push: saving subscription nickname=%q id=%s: %v", nickname, subscriptionID(request.Subscription), err)
			http.Error(w, "could not save subscription", http.StatusInternalServerError)
			return
		}
		log.Printf("push: saved subscription nickname=%q id=%s total=%d", nickname, subscriptionID(request.Subscription), push.count())
		w.WriteHeader(http.StatusNoContent)
	}
}
