package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type titleHistoryEntry struct {
	Title     string    `json:"title"`
	UseCount  int       `json:"useCount"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type titleHistoryFile struct {
	Titles []titleHistoryEntry `json:"titles"`
}

// TitleHistoryStore keeps user-entered product names outside the temporary
// catalog, so autocomplete survives catalog replacement and app restarts.
type TitleHistoryStore struct {
	mu      sync.RWMutex
	path    string
	entries []titleHistoryEntry
}

func NewTitleHistoryStore(path string) *TitleHistoryStore {
	store := &TitleHistoryStore{path: path, entries: []titleHistoryEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logEvent("title_history", "history could not be read", map[string]any{"error": err.Error()})
		}
		return store
	}

	var saved titleHistoryFile
	if err := json.Unmarshal(data, &saved); err != nil {
		logEvent("title_history", "history JSON is invalid; starting with an empty history", map[string]any{"error": err.Error()})
		return store
	}
	store.entries = saved.Titles
	return store
}

func (s *TitleHistoryStore) Remember(title string) error {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return nil
	}

	normalized := normalizeTitleSearch(title)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	found := -1
	for index := range s.entries {
		if normalizeTitleSearch(s.entries[index].Title) == normalized {
			found = index
			break
		}
	}
	if found >= 0 {
		s.entries[found].Title = title
		s.entries[found].UseCount++
		s.entries[found].UpdatedAt = now
	} else {
		s.entries = append(s.entries, titleHistoryEntry{Title: title, UseCount: 1, UpdatedAt: now})
	}

	return s.saveLocked()
}

func (s *TitleHistoryStore) Delete(title string) (bool, error) {
	normalized := normalizeTitleSearch(title)
	if normalized == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.entries {
		if normalizeTitleSearch(s.entries[index].Title) != normalized {
			continue
		}
		s.entries = append(s.entries[:index], s.entries[index+1:]...)
		return true, s.saveLocked()
	}
	return false, nil
}

func (s *TitleHistoryStore) Suggestions(query string) []string {
	query = normalizeTitleSearch(query)
	if query == "" {
		return []string{}
	}

	type match struct {
		entry titleHistoryEntry
		rank  int
	}
	matches := make([]match, 0)
	queryWords := strings.Fields(query)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.entries {
		title := normalizeTitleSearch(entry.Title)
		rank := titleMatchRank(title, query, queryWords)
		if rank < 0 {
			continue
		}
		matches = append(matches, match{entry: entry, rank: rank})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		if matches[i].entry.UseCount != matches[j].entry.UseCount {
			return matches[i].entry.UseCount > matches[j].entry.UseCount
		}
		if !matches[i].entry.UpdatedAt.Equal(matches[j].entry.UpdatedAt) {
			return matches[i].entry.UpdatedAt.After(matches[j].entry.UpdatedAt)
		}
		return len(matches[i].entry.Title) < len(matches[j].entry.Title)
	})

	result := make([]string, len(matches))
	for index, candidate := range matches {
		result[index] = candidate.entry.Title
	}
	return result
}

func (s *TitleHistoryStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(titleHistoryFile{Titles: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (a *App) handleTitleSuggestions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string][]string{
			"suggestions": a.titleHistory.Suggestions(r.URL.Query().Get("q")),
		})
	case http.MethodDelete:
		title := strings.TrimSpace(r.URL.Query().Get("title"))
		if title == "" {
			writeError(w, http.StatusBadRequest, "naslov je obavezan")
			return
		}
		deleted, err := a.titleHistory.Delete(title)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "brisanje naslova nije uspjelo")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) rememberTitle(title string) {
	if a.titleHistory == nil {
		return
	}
	if err := a.titleHistory.Remember(title); err != nil {
		logEvent("title_history", "title could not be saved", map[string]any{"error": err.Error()})
	}
}

func titleMatchRank(title string, query string, queryWords []string) int {
	if title == query {
		return 0
	}
	if strings.HasPrefix(title, query) {
		return 100 + len(title) - len(query)
	}
	for _, word := range strings.Fields(title) {
		if strings.HasPrefix(word, query) {
			return 300 + len(word) - len(query)
		}
	}
	if position := strings.Index(title, query); position >= 0 {
		return 500 + position
	}
	for _, queryWord := range queryWords {
		if !strings.Contains(title, queryWord) {
			return -1
		}
	}
	return 700
}

func normalizeTitleSearch(value string) string {
	replacer := strings.NewReplacer(
		"č", "c", "ć", "c", "ž", "z", "š", "s", "đ", "d",
		"Č", "c", "Ć", "c", "Ž", "z", "Š", "s", "Đ", "d",
	)
	return strings.ToLower(strings.Join(strings.Fields(replacer.Replace(value)), " "))
}
