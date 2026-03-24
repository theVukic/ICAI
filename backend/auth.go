package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	loginConfigURL                = "https://thevukic.github.io/ICAIL/login.json"
	authCookieName                = "icai_session"
	authRememberDuration          = 30 * 24 * time.Hour
	authSessionDuration           = 8 * time.Hour
	authValidationInterval        = 20 * time.Second
	authOfflineGracePeriod        = 10 * time.Minute
	authSaltSize                  = 16
	authArgonTime          uint32 = 3
	authArgonMemory        uint32 = 64 * 1024
	authArgonThreads       uint8  = 4
	authArgonKeyBytes      uint32 = 32
)

type pepperConfig struct {
	ConfigVersion int    `json:"config_version"`
	Pepper        string `json:"pepper,omitempty"`
	PepperRaw     string `json:"pepper_raw,omitempty"`
	PepperValue   string `json:"pepper_value,omitempty"`
	Secret        string `json:"secret,omitempty"`
	PepperHash    string `json:"pepper_hash"`
	SaltPepper    string `json:"salt_pepper,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type remoteLoginConfig struct {
	ConfigVersion int               `json:"config_version"`
	Users         []remoteLoginUser `json:"users"`
}

type remoteLoginUser struct {
	UsernameLookupHash string `json:"username_lookup_hash"`
	UsernameHash       string `json:"username_hash"`
	PasswordHash       string `json:"password_hash"`
	SaltUsername       string `json:"salt_username"`
	SaltPassword       string `json:"salt_password"`
	Enabled            bool   `json:"enabled"`
	Role               string `json:"role"`
	ExpiresAt          string `json:"expires_at"`
	Note               string `json:"note"`
}

type appSession struct {
	Token                string
	Username             string
	Role                 string
	Note                 string
	UsernameLookupHash   string
	PasswordHashSnapshot string
	ExpiresAt            time.Time
	RememberMe           bool
	CreatedAt            time.Time
	LastValidatedAt      time.Time
}

type AuthManager struct {
	pepperPath         string
	sessionsPath       string
	legacySessionsPath string
	httpClient         *http.Client

	mu       sync.RWMutex
	sessions map[string]appSession
}

func NewAuthManager(searchRoots []string) *AuthManager {
	manager := &AuthManager{
		pepperPath:         resolveFirstExistingFile(searchRoots, "pepper.json"),
		sessionsPath:       resolveSessionStoragePath(searchRoots),
		legacySessionsPath: resolveLegacySessionStoragePath(searchRoots),
		httpClient:         &http.Client{Timeout: 12 * time.Second},
		sessions:           make(map[string]appSession),
	}

	manager.loadPersistedSessions()

	return manager
}

func (m *AuthManager) loadPepper() (string, error) {
	if strings.TrimSpace(m.pepperPath) == "" {
		return "", errors.New("pepper.json nije pronadjen")
	}

	data, err := os.ReadFile(m.pepperPath)
	if err != nil {
		return "", fmt.Errorf("ne mogu ucitati pepper.json: %w", err)
	}

	var config pepperConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("pepper.json nije validan: %w", err)
	}

	pepper := firstNonEmptyTrimmed(
		strings.TrimSpace(config.Pepper),
		strings.TrimSpace(config.PepperRaw),
		strings.TrimSpace(config.PepperValue),
		strings.TrimSpace(config.Secret),
	)
	if pepper == "" {
		return "", errors.New("pepper.json ne sadrzi lokalni raw pepper")
	}

	if strings.TrimSpace(config.PepperHash) != "" && strings.TrimSpace(config.SaltPepper) != "" {
		saltPepper, err := base64.StdEncoding.DecodeString(config.SaltPepper)
		if err != nil {
			return "", errors.New("pepper.json ima neispravan salt_pepper")
		}

		calculated := hashWithArgon2(pepper, "", saltPepper)
		if !constantTimeBase64StringEqual(calculated, config.PepperHash) {
			return "", errors.New("lokalni pepper iz pepper.json ne odgovara pepper_hash zapisu")
		}
	}

	return pepper, nil
}

func (m *AuthManager) fetchRemoteUsers(ctx context.Context) ([]remoteLoginUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginConfigURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ne mogu preuzeti login.json: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login.json vraca status %d", resp.StatusCode)
	}

	var payload remoteLoginConfig
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("login.json nije validan JSON: %w", err)
	}

	return payload.Users, nil
}

func (m *AuthManager) VerifyLogin(ctx context.Context, username, password string) (remoteLoginUser, error) {
	pepper, err := m.loadPepper()
	if err != nil {
		return remoteLoginUser{}, err
	}

	users, err := m.fetchRemoteUsers(ctx)
	if err != nil {
		return remoteLoginUser{}, err
	}

	lookupHash := makeUsernameLookupHash(username, pepper)
	user, found := findUserByLookupHash(users, lookupHash)
	if !found {
		return remoteLoginUser{}, errors.New("pogresan username ili password")
	}

	if !user.Enabled {
		return remoteLoginUser{}, errors.New("korisnik je onemogucen")
	}

	expiresAt, err := time.Parse(time.RFC3339, user.ExpiresAt)
	if err != nil {
		return remoteLoginUser{}, errors.New("login.json ima neispravan expires_at")
	}
	if time.Now().UTC().After(expiresAt) {
		return remoteLoginUser{}, errors.New("korisnicki nalog je istekao")
	}

	usernameSalt, err := base64.StdEncoding.DecodeString(user.SaltUsername)
	if err != nil {
		return remoteLoginUser{}, errors.New("login.json ima neispravan salt_username")
	}

	passwordSalt, err := base64.StdEncoding.DecodeString(user.SaltPassword)
	if err != nil {
		return remoteLoginUser{}, errors.New("login.json ima neispravan salt_password")
	}

	calculatedUsernameHash := hashWithArgon2(username, pepper, usernameSalt)
	if !constantTimeBase64StringEqual(calculatedUsernameHash, user.UsernameHash) {
		return remoteLoginUser{}, errors.New("pogresan username ili password")
	}

	calculatedPasswordHash := hashWithArgon2(password, pepper, passwordSalt)
	if !constantTimeBase64StringEqual(calculatedPasswordHash, user.PasswordHash) {
		return remoteLoginUser{}, errors.New("pogresan username ili password")
	}

	return user, nil
}

func (m *AuthManager) CreateSession(username string, user remoteLoginUser, rememberMe bool) (appSession, error) {
	token, err := generateSessionToken()
	if err != nil {
		return appSession{}, err
	}

	duration := authSessionDuration
	if rememberMe {
		duration = authRememberDuration
	}

	session := appSession{
		Token:                token,
		Username:             username,
		Role:                 user.Role,
		Note:                 user.Note,
		UsernameLookupHash:   user.UsernameLookupHash,
		PasswordHashSnapshot: user.PasswordHash,
		ExpiresAt:            time.Now().UTC().Add(duration),
		RememberMe:           rememberMe,
		CreatedAt:            time.Now().UTC(),
		LastValidatedAt:      time.Now().UTC(),
	}

	m.mu.Lock()
	m.sessions[token] = session
	if rememberMe {
		if err := m.persistRememberedSessionsLocked(); err != nil {
			delete(m.sessions, token)
			m.mu.Unlock()
			return appSession{}, err
		}
	}
	m.mu.Unlock()

	return session, nil
}

func (m *AuthManager) SessionFromRequest(r *http.Request) (appSession, bool) {
	if r == nil {
		return appSession{}, false
	}

	cookie, err := r.Cookie(authCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return appSession{}, false
	}

	return m.GetSession(cookie.Value)
}

func (m *AuthManager) GetSession(token string) (appSession, bool) {
	m.mu.RLock()
	session, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return appSession{}, false
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		m.DeleteSession(token)
		return appSession{}, false
	}

	if m.shouldValidateSession(session) {
		validatedSession, valid := m.validateSession(token, session)
		if !valid {
			return appSession{}, false
		}
		session = validatedSession
	}

	return session, true
}

func (m *AuthManager) DeleteSession(token string) {
	if strings.TrimSpace(token) == "" {
		return
	}

	m.mu.Lock()
	session, existed := m.sessions[token]
	if existed {
		delete(m.sessions, token)
	}
	shouldPersist := existed && session.RememberMe
	if shouldPersist {
		_ = m.persistRememberedSessionsLocked()
	}
	m.mu.Unlock()
}

func (m *AuthManager) DeleteSessionFromRequest(r *http.Request) {
	if r == nil {
		return
	}

	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return
	}

	m.DeleteSession(cookie.Value)
}

func (m *AuthManager) WriteSessionCookie(w http.ResponseWriter, session appSession) {
	cookie := &http.Cookie{
		Name:     authCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	if session.RememberMe {
		cookie.Expires = session.ExpiresAt
		cookie.MaxAge = int(time.Until(session.ExpiresAt).Seconds())
	}

	http.SetCookie(w, cookie)
}

func (m *AuthManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func generateSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("ne mogu generisati session token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func findUserByLookupHash(users []remoteLoginUser, lookupHash string) (remoteLoginUser, bool) {
	for _, user := range users {
		if constantTimeBase64StringEqual(user.UsernameLookupHash, lookupHash) {
			return user, true
		}
	}

	return remoteLoginUser{}, false
}

func makeUsernameLookupHash(username, pepper string) string {
	sum := sha256.Sum256([]byte(username + pepper))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func hashWithArgon2(input, pepper string, salt []byte) string {
	hash := argon2.IDKey([]byte(input+pepper), salt, authArgonTime, authArgonMemory, authArgonThreads, authArgonKeyBytes)
	return base64.StdEncoding.EncodeToString(hash)
}

func constantTimeBase64StringEqual(left, right string) bool {
	leftBytes, leftErr := base64.StdEncoding.DecodeString(left)
	rightBytes, rightErr := base64.StdEncoding.DecodeString(right)
	if leftErr != nil || rightErr != nil {
		return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
	}

	if len(leftBytes) != len(rightBytes) {
		return false
	}

	return subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

func resolveFirstExistingFile(searchRoots []string, fileName string) string {
	for _, root := range searchRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}

		candidate := root
		if filepath.Base(root) != fileName {
			candidate = filepath.Join(root, fileName)
		}

		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	return ""
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func (m *AuthManager) shouldValidateSession(session appSession) bool {
	if strings.TrimSpace(session.UsernameLookupHash) == "" || strings.TrimSpace(session.PasswordHashSnapshot) == "" {
		return true
	}

	if session.LastValidatedAt.IsZero() {
		return true
	}

	return time.Since(session.LastValidatedAt) >= authValidationInterval
}

func (m *AuthManager) validateSession(token string, session appSession) (appSession, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	users, err := m.fetchRemoteUsers(ctx)
	if err != nil {
		if !session.LastValidatedAt.IsZero() && time.Since(session.LastValidatedAt) <= authOfflineGracePeriod {
			return session, true
		}

		m.DeleteSession(token)
		return appSession{}, false
	}

	user, found := findUserByLookupHash(users, session.UsernameLookupHash)
	if !found || !user.Enabled || !constantTimeBase64StringEqual(user.PasswordHash, session.PasswordHashSnapshot) {
		m.DeleteSession(token)
		return appSession{}, false
	}

	expiresAt, err := time.Parse(time.RFC3339, user.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		m.DeleteSession(token)
		return appSession{}, false
	}

	session.Role = user.Role
	session.Note = user.Note
	session.LastValidatedAt = time.Now().UTC()

	m.mu.Lock()
	currentSession, ok := m.sessions[token]
	if !ok {
		m.mu.Unlock()
		return appSession{}, false
	}

	currentSession.Role = session.Role
	currentSession.Note = session.Note
	currentSession.LastValidatedAt = session.LastValidatedAt
	m.sessions[token] = currentSession
	if currentSession.RememberMe {
		_ = m.persistRememberedSessionsLocked()
	}
	m.mu.Unlock()

	return currentSession, true
}

func (m *AuthManager) loadPersistedSessions() {
	sessionPath := firstNonEmptyTrimmed(
		findFirstExistingPath(m.sessionsPath, m.legacySessionsPath),
		m.sessionsPath,
	)
	if sessionPath == "" {
		return
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}

	var persisted []appSession
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}

	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, session := range persisted {
		if !session.RememberMe || strings.TrimSpace(session.Token) == "" {
			continue
		}

		if now.After(session.ExpiresAt) {
			continue
		}

		m.sessions[session.Token] = session
	}

	_ = m.persistRememberedSessionsLocked()

	if sessionPath != m.sessionsPath && strings.TrimSpace(m.sessionsPath) != "" {
		_ = os.Remove(sessionPath)
	}
}

func (m *AuthManager) persistRememberedSessionsLocked() error {
	if strings.TrimSpace(m.sessionsPath) == "" {
		return nil
	}

	now := time.Now().UTC()
	persisted := make([]appSession, 0, len(m.sessions))

	for token, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, token)
			continue
		}

		if session.RememberMe {
			persisted = append(persisted, session)
		}
	}

	if len(persisted) == 0 {
		if err := os.Remove(m.sessionsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("ne mogu obrisati session storage: %w", err)
		}
		return nil
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("ne mogu serijalizovati remembered sessione: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(m.sessionsPath), 0o755); err != nil {
		return fmt.Errorf("ne mogu kreirati direktorij za sessione: %w", err)
	}

	tempPath := m.sessionsPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("ne mogu sacuvati remembered sessione: %w", err)
	}

	if err := os.Rename(tempPath, m.sessionsPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("ne mogu zamijeniti storage remembered sessiona: %w", err)
	}

	return nil
}

func resolveSessionStoragePath(searchRoots []string) string {
	for _, candidate := range buildSessionStorageCandidates(searchRoots, "session.json") {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	candidates := buildSessionStorageCandidates(searchRoots, "session.json")
	if len(candidates) > 0 {
		return candidates[0]
	}

	return "session.json"
}

func resolveLegacySessionStoragePath(searchRoots []string) string {
	for _, candidate := range buildSessionStorageCandidates(searchRoots, "auth_sessions.json") {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	candidates := buildSessionStorageCandidates(searchRoots, "auth_sessions.json")
	if len(candidates) > 0 {
		return candidates[0]
	}

	return ""
}

func buildSessionStorageCandidates(searchRoots []string, fileName string) []string {
	candidates := make([]string, 0, len(searchRoots)*2)
	seen := make(map[string]struct{})

	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	for _, root := range searchRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}

		if filepath.Ext(root) != "" {
			root = filepath.Dir(root)
		}

		if strings.EqualFold(filepath.Base(root), "backend") {
			addCandidate(filepath.Join(filepath.Dir(root), fileName))
		}

		addCandidate(filepath.Join(root, fileName))
	}

	return candidates
}

func findFirstExistingPath(paths ...string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	return ""
}
