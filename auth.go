package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	authCookieName = "backup-chat-session"
	authCookieAge  = 30 * 24 * time.Hour
)

type authConfig struct {
	username string
	password string
}

func newAuthConfig() (authConfig, error) {
	config := authConfig{
		username: strings.TrimSpace(os.Getenv("AUTH_USERNAME")),
		password: strings.TrimSpace(os.Getenv("AUTH_PASSWORD")),
	}
	if config.username == "" {
		return authConfig{}, fmt.Errorf("AUTH_USERNAME must be set")
	}
	if config.password == "" {
		return authConfig{}, fmt.Errorf("AUTH_PASSWORD must be set")
	}
	return config, nil
}

func (config authConfig) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		if config.validSession(r, time.Now()) {
			next.ServeHTTP(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(username), []byte(config.username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(config.password)) != 1 {
			if strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="Backup Chat", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, config.sessionCookie(r, time.Now()))
		next.ServeHTTP(w, r)
	})
}

func (config authConfig) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil &&
			subtle.ConstantTimeCompare([]byte(r.FormValue("username")), []byte(config.username)) == 1 &&
			subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(config.password)) == 1 {
			http.SetCookie(w, config.sessionCookie(r, time.Now()))
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	page, err := webFiles.ReadFile("web/login.html")
	if err != nil {
		http.Error(w, "login page unavailable", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("error") == "1" {
		page = []byte(strings.Replace(string(page), "<!-- ERROR -->", "Invalid username or password.", 1))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func (config authConfig) sessionCookie(r *http.Request, now time.Time) *http.Cookie {
	expires := now.Add(authCookieAge).Unix()
	payload := config.username + "." + strconv.FormatInt(expires, 10)
	return &http.Cookie{
		Name:     authCookieName,
		Value:    config.sign(payload),
		Path:     "/",
		Expires:  time.Unix(expires, 0),
		MaxAge:   int(authCookieAge / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	}
}

func (config authConfig) validSession(r *http.Request, now time.Time) bool {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return false
	}
	parts := strings.Split(string(decoded), ".")
	if len(parts) != 3 || parts[0] != config.username {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() >= expires {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(config.signature(parts[0]+"."+parts[1])))
}

func (config authConfig) sign(payload string) string {
	value := payload + "." + config.signature(payload)
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func (config authConfig) signature(payload string) string {
	digest := hmac.New(sha256.New, []byte(config.password))
	_, _ = digest.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}
