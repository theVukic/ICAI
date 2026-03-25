package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const olxBaseURL = "https://api.olx.ba"

type OLXCategoryService struct {
	httpClient       *http.Client
	imageHTTPClient  *http.Client
	uploadHTTPClient *http.Client
	token            string
	tokenName        string
	tokenPath        string
	tokens           []namedTokenFile
	me               SessionInfoResponse
}

type tokenFilePayload struct {
	Name       string           `json:"name,omitempty"`
	Token      string           `json:"token,omitempty"`
	ActiveName string           `json:"activeName,omitempty"`
	Tokens     []namedTokenFile `json:"tokens"`
}

type namedTokenFile struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

type olxDataResponse[T any] struct {
	Data T `json:"data"`
}

type olxAPIErrorEnvelope struct {
	Error *struct {
		Type    string `json:"type"`
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

type olxMeResponse struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func NewOLXCategoryService(searchRoots []string) *OLXCategoryService {
	tokenPath, tokenName, token, tokens := loadOLXTokenConfig(searchRoots)
	logEvent("olx", "token config discovered", map[string]any{
		"active_profile": tokenName,
		"available_profiles": func() []string {
			names := make([]string, 0, len(tokens))
			for _, entry := range tokens {
				names = append(names, entry.Name)
			}
			return names
		}(),
		"token_configured": strings.TrimSpace(token) != "",
		"token_path":       tokenPath,
		"token_preview":    maskSecret(token),
	})
	return &OLXCategoryService{
		httpClient:       &http.Client{Timeout: 12 * time.Second},
		imageHTTPClient:  &http.Client{Timeout: 30 * time.Second},
		uploadHTTPClient: &http.Client{Timeout: 45 * time.Second},
		token:            token,
		tokenName:        tokenName,
		tokenPath:        tokenPath,
		tokens:           tokens,
	}
}

func (s *OLXCategoryService) HasToken() bool {
	return strings.TrimSpace(s.token) != ""
}

func (s *OLXCategoryService) SessionInfo() SessionInfoResponse {
	if !s.HasToken() {
		return SessionInfoResponse{
			TokenConfigured: false,
			AvailableTokens: s.AvailableProfiles(),
		}
	}

	response := s.me
	response.TokenConfigured = true
	if strings.TrimSpace(response.Username) == "" {
		response.Username = "Nepoznato"
	}
	if strings.TrimSpace(response.ProfileName) == "" {
		response.ProfileName = s.ActiveProfileName()
	}
	response.AvailableTokens = s.AvailableProfiles()
	return response
}

func (s *OLXCategoryService) AvailableProfiles() []string {
	if len(s.tokens) == 0 {
		if strings.TrimSpace(s.tokenName) == "" {
			return []string{}
		}
		return []string{strings.TrimSpace(s.tokenName)}
	}

	seen := make(map[string]struct{}, len(s.tokens))
	profiles := make([]string, 0, len(s.tokens))
	for _, entry := range s.tokens {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = "default"
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		profiles = append(profiles, name)
	}

	return profiles
}

func (s *OLXCategoryService) SwitchActiveProfile(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	logEvent("olx", "switch active profile requested", map[string]any{
		"requested_profile": name,
	})
	if name == "" {
		return fmt.Errorf("naziv tokena je obavezan")
	}
	if strings.TrimSpace(s.tokenPath) == "" {
		return fmt.Errorf("backend/token.json nije pronadjen")
	}

	content, err := os.ReadFile(s.tokenPath)
	if err != nil {
		return fmt.Errorf("ne mogu procitati token.json: %w", err)
	}

	var payload tokenFilePayload
	if err := json.Unmarshal(content, &payload); err != nil {
		return fmt.Errorf("token.json nije validan")
	}

	if len(payload.Tokens) == 0 {
		return fmt.Errorf("token.json ne sadrzi vise tokena za izbor")
	}

	var selected *namedTokenFile
	for i := range payload.Tokens {
		if strings.EqualFold(strings.TrimSpace(payload.Tokens[i].Name), name) {
			selected = &payload.Tokens[i]
			break
		}
	}
	if selected == nil || strings.TrimSpace(selected.Token) == "" {
		return fmt.Errorf("trazeni OLX token nije pronadjen")
	}

	reordered := make([]namedTokenFile, 0, len(payload.Tokens))
	reordered = append(reordered, *selected)
	for _, entry := range payload.Tokens {
		if strings.EqualFold(strings.TrimSpace(entry.Name), strings.TrimSpace(selected.Name)) {
			continue
		}
		reordered = append(reordered, entry)
	}

	payload.Tokens = reordered
	if err := saveOLXTokenConfig(s.tokenPath, payload.Tokens); err != nil {
		return fmt.Errorf("ne mogu sacuvati token.json: %w", err)
	}

	s.tokenName = strings.TrimSpace(selected.Name)
	s.token = strings.TrimPrefix(strings.TrimSpace(selected.Token), "Bearer ")
	s.tokens = reordered
	s.me = SessionInfoResponse{}

	loadCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := s.LoadMe(loadCtx); err != nil {
		return err
	}

	logEvent("olx", "active profile switched", map[string]any{
		"active_profile":     s.tokenName,
		"available_profiles": s.AvailableProfiles(),
		"token_preview":      maskSecret(s.token),
	})

	return nil
}

func (s *OLXCategoryService) ActiveProfileName() string {
	if strings.TrimSpace(s.tokenName) != "" {
		return strings.TrimSpace(s.tokenName)
	}
	if strings.TrimSpace(s.me.Username) != "" {
		return strings.TrimSpace(s.me.Username)
	}
	if strings.TrimSpace(s.me.ProfileName) != "" &&
		!strings.EqualFold(strings.TrimSpace(s.me.ProfileName), "default") {
		return strings.TrimSpace(s.me.ProfileName)
	}
	return "default"
}

func (s *OLXCategoryService) LoadMe(ctx context.Context) error {
	if !s.HasToken() {
		return nil
	}

	logEvent("olx", "loading profile info", map[string]any{
		"active_profile": s.ActiveProfileName(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, olxBaseURL+"/me", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OLX /me request nije uspeo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OLX /me greška (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var direct olxMeResponse
	if err := json.Unmarshal(body, &direct); err == nil && strings.TrimSpace(direct.Username) != "" {
		s.me = SessionInfoResponse{
			TokenConfigured: true,
			Username:        direct.Username,
			FullName:        strings.TrimSpace(direct.FirstName + " " + direct.LastName),
			ProfileName:     firstNonEmpty(strings.TrimSpace(s.tokenName), strings.TrimSpace(direct.Username), "default"),
		}
		logEvent("olx", "profile info loaded", map[string]any{
			"active_profile": s.ActiveProfileName(),
			"username":       s.me.Username,
			"full_name":      s.me.FullName,
		})
		return nil
	}

	var wrapped olxDataResponse[olxMeResponse]
	if err := json.Unmarshal(body, &wrapped); err == nil && strings.TrimSpace(wrapped.Data.Username) != "" {
		s.me = SessionInfoResponse{
			TokenConfigured: true,
			Username:        wrapped.Data.Username,
			FullName:        strings.TrimSpace(wrapped.Data.FirstName + " " + wrapped.Data.LastName),
			ProfileName:     firstNonEmpty(strings.TrimSpace(s.tokenName), strings.TrimSpace(wrapped.Data.Username), "default"),
		}
		logEvent("olx", "profile info loaded", map[string]any{
			"active_profile": s.ActiveProfileName(),
			"username":       s.me.Username,
			"full_name":      s.me.FullName,
		})
		return nil
	}

	return fmt.Errorf("OLX /me odgovor nema username")
}

func (s *OLXCategoryService) FindCategories(ctx context.Context, name string) ([]OLXCategorySuggestion, error) {
	if strings.TrimSpace(name) == "" {
		return []OLXCategorySuggestion{}, nil
	}

	reqURL := fmt.Sprintf("%s/categories/find?name=%s", olxBaseURL, url.QueryEscape(name))
	var result []OLXCategorySuggestion
	if err := s.getJSONWithRetry(ctx, reqURL, &result, "find categories"); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *OLXCategoryService) SuggestCategories(ctx context.Context, keyword string) ([]OLXCategorySuggestion, error) {
	if strings.TrimSpace(keyword) == "" {
		return []OLXCategorySuggestion{}, nil
	}

	reqURL := fmt.Sprintf("%s/categories/suggest?keyword=%s", olxBaseURL, url.QueryEscape(keyword))
	var wrapped olxDataResponse[[]OLXCategorySuggestion]
	if err := s.getJSONWithRetry(ctx, reqURL, &wrapped, "suggest categories"); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategory(ctx context.Context, categoryID int) (*OLXCategoryNode, error) {
	reqURL := fmt.Sprintf("%s/category/%d", olxBaseURL, categoryID)
	var wrapped olxDataResponse[OLXCategoryNode]
	if err := s.getJSONWithRetry(ctx, reqURL, &wrapped, "get category"); err != nil {
		return nil, err
	}
	return &wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryAttributes(ctx context.Context, categoryID int) ([]OLXCategoryAttribute, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/attributes", olxBaseURL, categoryID)
	var wrapped olxDataResponse[[]OLXCategoryAttribute]
	if err := s.getJSONWithRetry(ctx, reqURL, &wrapped, "get category attributes"); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryBrands(ctx context.Context, categoryID int) ([]OLXBrand, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/brands", olxBaseURL, categoryID)
	var wrapped olxDataResponse[[]OLXBrand]
	if err := s.getJSONWithRetry(ctx, reqURL, &wrapped, "get category brands"); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryModels(ctx context.Context, categoryID int, brandID int) ([]OLXModel, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/brands/%d/models", olxBaseURL, categoryID, brandID)
	var wrapped olxDataResponse[[]OLXModel]
	if err := s.getJSONWithRetry(ctx, reqURL, &wrapped, "get category models"); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) getJSON(ctx context.Context, requestURL string, dest any) error {
	if !s.HasToken() {
		return fmt.Errorf("OLX token nije podešen. Zalijepi ga u backend/token.json")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OLX request nije uspeo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return formatOLXAPIError(resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("OLX odgovor nije validan JSON: %w", err)
	}

	return nil
}

func (s *OLXCategoryService) getJSONWithRetry(ctx context.Context, requestURL string, dest any, operation string) error {
	var err error
	for attempt := 1; attempt <= listingLimitsAttempts; attempt++ {
		err = s.getJSON(ctx, requestURL, dest)
		if err == nil {
			return nil
		}
		if !isRetriableOLXRequestError(err) || attempt == listingLimitsAttempts {
			return err
		}
		if waitErr := waitForRetry(ctx, attempt); waitErr != nil {
			return err
		}
		log.Printf("[OLX] %s retry attempt=%d/%d error=%v", operation, attempt+1, listingLimitsAttempts, err)
	}
	return err
}

func formatOLXAPIError(statusCode int, body []byte) error {
	trimmedBody := strings.TrimSpace(string(body))

	var payload olxAPIErrorEnvelope
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil {
		errorType := strings.TrimSpace(payload.Error.Type)
		message := strings.TrimSpace(payload.Error.Message)
		if statusCode >= http.StatusInternalServerError || strings.EqualFold(errorType, "internal_error") {
			return fmt.Errorf("OLX je privremeno nedostupan (%d). Pokušaj ponovo za nekoliko trenutaka", statusCode)
		}
		if message != "" {
			return fmt.Errorf("OLX API greška (%d): %s", statusCode, message)
		}
	}

	if statusCode >= http.StatusInternalServerError {
		return fmt.Errorf("OLX je privremeno nedostupan (%d). Pokušaj ponovo za nekoliko trenutaka", statusCode)
	}

	return fmt.Errorf("OLX API greška (%d): %s", statusCode, trimmedBody)
}

func loadOLXTokenConfig(searchRoots []string) (string, string, string, []namedTokenFile) {
	if envToken := strings.TrimSpace(os.Getenv("OLX_BEARER_TOKEN")); envToken != "" {
		return "", strings.TrimSpace(os.Getenv("OLX_TOKEN_NAME")), envToken, []namedTokenFile{}
	}

	for _, root := range searchRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}

		tokenPath := filepath.Join(root, "token.json")
		content, err := os.ReadFile(tokenPath)
		if err != nil {
			continue
		}

		var payload tokenFilePayload
		if err := json.Unmarshal(content, &payload); err != nil {
			var entries []namedTokenFile
			if arrayErr := json.Unmarshal(content, &entries); arrayErr != nil {
				continue
			}
			payload.Tokens = entries
		}

		if len(payload.Tokens) == 0 {
			if token := strings.TrimSpace(payload.Token); token != "" {
				return tokenPath, strings.TrimSpace(payload.Name), strings.TrimPrefix(token, "Bearer "), payload.Tokens
			}
			continue
		}

		entries := payload.Tokens
		if activeName := strings.TrimSpace(payload.ActiveName); activeName != "" {
			for i, entry := range payload.Tokens {
				if !strings.EqualFold(strings.TrimSpace(entry.Name), activeName) {
					continue
				}

				entries = append([]namedTokenFile{entry}, append(payload.Tokens[:i:i], payload.Tokens[i+1:]...)...)
				break
			}
		}

		for _, entry := range entries {
			token := strings.TrimSpace(entry.Token)
			if token == "" {
				continue
			}
			return tokenPath, strings.TrimSpace(entry.Name), strings.TrimPrefix(token, "Bearer "), payload.Tokens
		}
	}

	return "", "", "", []namedTokenFile{}
}

func saveOLXTokenConfig(tokenPath string, tokens []namedTokenFile) error {
	payload := tokenFilePayload{
		Tokens: tokens,
	}

	updated, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("ne mogu upisati token.json")
	}

	updated = append(updated, '\n')
	if err := os.WriteFile(tokenPath, updated, 0o600); err != nil {
		return err
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func (a *App) handleCategoryItem(w http.ResponseWriter, r *http.Request) {
	towKod, err := itemCodeFromPath(strings.TrimPrefix(r.URL.Path, "/api/category/item"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, getErr := a.store.GetItemByTowKod(towKod)
		if getErr != nil {
			if errors.Is(getErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, getErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, getErr.Error())
			return
		}

		writeJSON(w, http.StatusOK, CategoryPageResponse{
			Item:            item,
			Categorization:  item.Categorization,
			TokenConfigured: a.olxCategories != nil && a.olxCategories.HasToken(),
		})
	case http.MethodPut:
		var req CategorySaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "neispravan JSON body")
			return
		}

		selection := ItemCategorySelection{
			Category:   req.Category,
			Brand:      req.Brand,
			Model:      req.Model,
			Attributes: req.Attributes,
		}

		item, saveErr := a.store.SaveCategorization(towKod, selection)
		if saveErr != nil {
			if errors.Is(saveErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, saveErr.Error())
				return
			}
			writeError(w, http.StatusBadRequest, saveErr.Error())
			return
		}

		logEvent("category", "single categorization saved", map[string]any{
			"brand_id": func() int {
				if req.Brand == nil {
					return 0
				}
				return req.Brand.ID
			}(),
			"category_id": func() int {
				if req.Category == nil {
					return 0
				}
				return req.Category.ID
			}(),
			"model_id": func() int {
				if req.Model == nil {
					return 0
				}
				return req.Model.ID
			}(),
			"tow_kod": towKod,
		})
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleCategoryBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CategoryBulkSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan JSON body")
		return
	}

	selection := ItemCategorySelection{
		Category:   req.Category,
		Brand:      req.Brand,
		Model:      req.Model,
		Attributes: req.Attributes,
	}

	items, err := a.store.SaveCategorizationBulk(req.ItemKeys, req.TowKods, selection)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logEvent("category", "bulk categorization saved", map[string]any{
		"category_id": func() int {
			if req.Category == nil {
				return 0
			}
			return req.Category.ID
		}(),
		"item_count": len(items),
		"tow_kods":   req.TowKods,
	})
	writeJSON(w, http.StatusOK, items)
}

func (a *App) handleOLXCategories(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/api/olx"), "/token") {
		a.handleOLXTokenSelection(w, r)
		return
	}

	if a.olxCategories == nil || !a.olxCategories.HasToken() {
		writeError(w, http.StatusBadRequest, "OLX token nije podešen. Zalijepi ga u backend/token.json")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/olx")
	switch {
	case strings.HasPrefix(path, "/categories/find"):
		name := r.URL.Query().Get("name")
		results, err := a.olxCategories.FindCategories(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, results)
	case strings.HasPrefix(path, "/categories/suggest"):
		keyword := r.URL.Query().Get("keyword")
		results, err := a.olxCategories.SuggestCategories(r.Context(), keyword)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, results)
	case strings.HasPrefix(path, "/categories/"):
		a.handleOLXCategoryDetails(w, r, strings.TrimPrefix(path, "/categories/"))
	default:
		http.NotFound(w, r)
	}
}

func (a *App) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var response SessionInfoResponse

	if a.olxCategories == nil {
		response = SessionInfoResponse{}
	} else {
		response = a.olxCategories.SessionInfo()
	}

	if a.auth != nil {
		if session, ok := a.auth.SessionFromRequest(r); ok {
			response.Authenticated = true
			response.AppUsername = session.Username
			response.AppRole = session.Role
		}
	}

	logEvent("session", "session info requested", map[string]any{
		"app_username":     response.AppUsername,
		"authenticated":    response.Authenticated,
		"available_tokens": response.AvailableTokens,
		"profile_name":     response.ProfileName,
		"token_configured": response.TokenConfigured,
		"username":         response.Username,
	})
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleOLXTokenSelection(w http.ResponseWriter, r *http.Request) {
	if a.olxCategories == nil {
		writeError(w, http.StatusBadRequest, "OLX token servis nije dostupan")
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenSwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan JSON body")
		return
	}

	if err := a.olxCategories.SwitchActiveProfile(r.Context(), req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	response := a.olxCategories.SessionInfo()
	if a.auth != nil {
		if session, ok := a.auth.SessionFromRequest(r); ok {
			response.Authenticated = true
			response.AppUsername = session.Username
			response.AppRole = session.Role
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleOLXCategoryDetails(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	categoryID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "category id nije validan")
		return
	}

	switch {
	case len(parts) == 1:
		category, getErr := a.olxCategories.GetCategory(r.Context(), categoryID)
		if getErr != nil {
			writeError(w, http.StatusBadGateway, getErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, category)
	case len(parts) == 2 && parts[1] == "attributes":
		attributes, getErr := a.olxCategories.GetCategoryAttributes(r.Context(), categoryID)
		if getErr != nil {
			writeError(w, http.StatusBadGateway, getErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, attributes)
	case len(parts) == 2 && parts[1] == "brands":
		brands, getErr := a.olxCategories.GetCategoryBrands(r.Context(), categoryID)
		if getErr != nil {
			writeError(w, http.StatusBadGateway, getErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, brands)
	case len(parts) == 4 && parts[1] == "brands" && parts[3] == "models":
		brandID, convErr := strconv.Atoi(parts[2])
		if convErr != nil {
			writeError(w, http.StatusBadRequest, "brand id nije validan")
			return
		}

		models, getErr := a.olxCategories.GetCategoryModels(r.Context(), categoryID, brandID)
		if getErr != nil {
			writeError(w, http.StatusBadGateway, getErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, models)
	default:
		http.NotFound(w, r)
	}
}
