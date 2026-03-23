package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	httpClient *http.Client
	token      string
	me         SessionInfoResponse
}

type tokenFilePayload struct {
	Token string `json:"token"`
}

type olxDataResponse[T any] struct {
	Data T `json:"data"`
}

type olxMeResponse struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func NewOLXCategoryService(searchRoots []string) *OLXCategoryService {
	return &OLXCategoryService{
		httpClient: &http.Client{Timeout: 12 * time.Second},
		token:      loadOLXToken(searchRoots),
	}
}

func (s *OLXCategoryService) HasToken() bool {
	return strings.TrimSpace(s.token) != ""
}

func (s *OLXCategoryService) SessionInfo() SessionInfoResponse {
	if !s.HasToken() {
		return SessionInfoResponse{TokenConfigured: false}
	}

	response := s.me
	response.TokenConfigured = true
	if strings.TrimSpace(response.Username) == "" {
		response.Username = "Nepoznato"
	}
	return response
}

func (s *OLXCategoryService) LoadMe(ctx context.Context) error {
	if !s.HasToken() {
		return nil
	}

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
		}
		return nil
	}

	var wrapped olxDataResponse[olxMeResponse]
	if err := json.Unmarshal(body, &wrapped); err == nil && strings.TrimSpace(wrapped.Data.Username) != "" {
		s.me = SessionInfoResponse{
			TokenConfigured: true,
			Username:        wrapped.Data.Username,
			FullName:        strings.TrimSpace(wrapped.Data.FirstName + " " + wrapped.Data.LastName),
		}
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
	if err := s.getJSON(ctx, reqURL, &result); err != nil {
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
	if err := s.getJSON(ctx, reqURL, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategory(ctx context.Context, categoryID int) (*OLXCategoryNode, error) {
	reqURL := fmt.Sprintf("%s/category/%d", olxBaseURL, categoryID)
	var wrapped olxDataResponse[OLXCategoryNode]
	if err := s.getJSON(ctx, reqURL, &wrapped); err != nil {
		return nil, err
	}
	return &wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryAttributes(ctx context.Context, categoryID int) ([]OLXCategoryAttribute, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/attributes", olxBaseURL, categoryID)
	var wrapped olxDataResponse[[]OLXCategoryAttribute]
	if err := s.getJSON(ctx, reqURL, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryBrands(ctx context.Context, categoryID int) ([]OLXBrand, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/brands", olxBaseURL, categoryID)
	var wrapped olxDataResponse[[]OLXBrand]
	if err := s.getJSON(ctx, reqURL, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func (s *OLXCategoryService) GetCategoryModels(ctx context.Context, categoryID int, brandID int) ([]OLXModel, error) {
	reqURL := fmt.Sprintf("%s/categories/%d/brands/%d/models", olxBaseURL, categoryID, brandID)
	var wrapped olxDataResponse[[]OLXModel]
	if err := s.getJSON(ctx, reqURL, &wrapped); err != nil {
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
		return fmt.Errorf("OLX API greška (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("OLX odgovor nije validan JSON: %w", err)
	}

	return nil
}

func loadOLXToken(searchRoots []string) string {
	if envToken := strings.TrimSpace(os.Getenv("OLX_BEARER_TOKEN")); envToken != "" {
		return envToken
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
			continue
		}

		token := strings.TrimSpace(payload.Token)
		if token != "" {
			return strings.TrimPrefix(token, "Bearer ")
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

		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleOLXCategories(w http.ResponseWriter, r *http.Request) {
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

	if a.olxCategories == nil {
		writeJSON(w, http.StatusOK, SessionInfoResponse{})
		return
	}

	writeJSON(w, http.StatusOK, a.olxCategories.SessionInfo())
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
