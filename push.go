package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
		return vapidConfig{
			Subject:    envOrDefault("VAPID_SUBJECT", "mailto:backup-chat@localhost"),
			PublicKey:  publicKey,
			PrivateKey: privateKey,
		}, nil
	}

	data, err := os.ReadFile(path)
	if err == nil {
		var config vapidConfig
		if json.Unmarshal(data, &config) == nil && config.PublicKey != "" && config.PrivateKey != "" {
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
	config := vapidConfig{
		Subject:    envOrDefault("VAPID_SUBJECT", "mailto:backup-chat@localhost"),
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}
	if err := writeJSONAtomically(path, config, 0600); err != nil {
		return vapidConfig{}, fmt.Errorf("saving VAPID keys: %w", err)
	}
	return config, nil
}

func (s *pushStore) loadSubscriptions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.subscriptionsPath)
	if errors.Is(err, os.ErrNotExist) {
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
	return nil
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

func (s *pushStore) send(message Message) {
	s.mu.Lock()
	subscriptions := append([]storedPushSubscription(nil), s.subscriptions...)
	s.mu.Unlock()
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
		if subscription.Nickname == message.Nickname {
			continue
		}
		response, err := webpush.SendNotification(payload, &subscription.Subscription, &webpush.Options{
			Subscriber:      s.config.Subject,
			VAPIDPublicKey:  s.config.PublicKey,
			VAPIDPrivateKey: s.config.PrivateKey,
			TTL:             60,
		})
		if err != nil {
			log.Printf("sending push notification: %v", err)
			continue
		}
		status := response.StatusCode
		_ = response.Body.Close()
		if status == http.StatusGone || status == http.StatusNotFound {
			if err := s.remove(subscription.Subscription.Endpoint); err != nil {
				log.Printf("removing expired push subscription: %v", err)
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"publicKey": push.config.PublicKey})
	}
}

func pushSubscribeHandler(push *pushStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request pushSubscribeRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid subscription", http.StatusBadRequest)
			return
		}
		nickname, err := validateNickname(request.Nickname)
		if err != nil || !validPushSubscription(request.Subscription) {
			http.Error(w, "invalid subscription", http.StatusBadRequest)
			return
		}
		if err := push.add(storedPushSubscription{Nickname: nickname, Subscription: request.Subscription}); err != nil {
			log.Printf("saving push subscription: %v", err)
			http.Error(w, "could not save subscription", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
