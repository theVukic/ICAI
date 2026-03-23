package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type olxListingAttributeValue struct {
	ID    int    `json:"id"`
	Value string `json:"value"`
}

type olxCreateListingRequest struct {
	Title            string                     `json:"title"`
	ShortDescription string                     `json:"short_description,omitempty"`
	Description      string                     `json:"description,omitempty"`
	Price            float64                    `json:"price,omitempty"`
	Available        bool                       `json:"available"`
	ListingType      string                     `json:"listing_type,omitempty"`
	State            string                     `json:"state,omitempty"`
	SKUNumber        string                     `json:"sku_number,omitempty"`
	BrandID          string                     `json:"brand_id,omitempty"`
	ModelID          string                     `json:"model_id,omitempty"`
	CategoryID       string                     `json:"category_id,omitempty"`
	Attributes       []olxListingAttributeValue `json:"attributes,omitempty"`
}

type olxListingResponse struct {
	ID int `json:"id"`
}

type olxWrappedListingResponse struct {
	Data olxListingResponse `json:"data"`
}

type olxImageUploadResponse struct {
	ID int `json:"id"`
}

type olxWrappedImageUploadResponse struct {
	Data []olxImageUploadResponse `json:"data"`
}

func (a *App) handleCatalogPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.olxCategories == nil || !a.olxCategories.HasToken() {
		writeError(w, http.StatusBadRequest, "OLX token nije podešen. Dodaj token i ime profila u backend/token.json.")
		return
	}

	var req CatalogPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "neispravan JSON body")
		return
	}

	log.Printf("[PUBLISH] request received selectedTowKods=%v", req.SelectedTowKods)

	response, status, err := a.publishCatalogItems(r.Context(), req.SelectedTowKods)
	if err != nil {
		log.Printf("[PUBLISH] request failed status=%d error=%v", status, err)
		writeError(w, status, err.Error())
		return
	}

	log.Printf(
		"[PUBLISH] completed published=%d skipped=%d failed=%d",
		len(response.Published),
		len(response.Skipped),
		len(response.Failed),
	)
	writeJSON(w, http.StatusOK, response)
}

func (a *App) publishCatalogItems(ctx context.Context, selectedTowKods []string) (CatalogPublishResponse, int, error) {
	state := a.store.State()
	if len(state.Items) == 0 {
		return CatalogPublishResponse{}, http.StatusBadRequest, fmt.Errorf("nema artikala za objavu")
	}

	selectedSet := make(map[string]struct{}, len(selectedTowKods))
	for _, towKod := range selectedTowKods {
		trimmed := strings.TrimSpace(towKod)
		if trimmed == "" {
			continue
		}
		selectedSet[trimmed] = struct{}{}
	}

	useSelection := len(selectedSet) > 0
	log.Printf("[PUBLISH] state totalItems=%d useSelection=%t", len(state.Items), useSelection)
	if useSelection {
		foundSelected := false
		for _, item := range state.Items {
			if _, ok := selectedSet[item.TowKod]; ok {
				foundSelected = true
				break
			}
		}
		if !foundSelected {
			return CatalogPublishResponse{}, http.StatusBadRequest, fmt.Errorf("nijedan selektovani artikal nije pronađen")
		}
	}

	document, err := a.loadDescriptionDocument()
	if err != nil {
		return CatalogPublishResponse{}, http.StatusInternalServerError, err
	}

	result := CatalogPublishResponse{
		Published: []PublishedListingResult{},
		Skipped:   []SkippedListingResult{},
		Failed:    []FailedListingResult{},
	}

	publishedTowKods := make([]string, 0)

	for _, item := range state.Items {
		if useSelection {
			if _, ok := selectedSet[item.TowKod]; !ok {
				continue
			}
		}

		log.Printf(
			"[PUBLISH] considering towKod=%s name=%q categoryID=%v images=%d price=%.2f",
			item.TowKod,
			item.NameB2B,
			func() any {
				if item.Categorization == nil || item.Categorization.Category == nil {
					return nil
				}
				return item.Categorization.Category.ID
			}(),
			len(item.Images),
			item.RetailPrice,
		)

		if item.Categorization == nil || item.Categorization.Category == nil || item.Categorization.Category.ID == 0 {
			log.Printf("[PUBLISH] skipping towKod=%s reason=no categorization", item.TowKod)
			result.Skipped = append(result.Skipped, SkippedListingResult{
				TowKod: item.TowKod,
				Title:  item.NameB2B,
				Reason: "artikal nema zelenu kategorizaciju",
			})
			continue
		}

		listingID, publishErr := a.publishSingleItem(ctx, item, document)
		if publishErr != nil {
			log.Printf("[PUBLISH] failed towKod=%s error=%v", item.TowKod, publishErr)
			result.Failed = append(result.Failed, FailedListingResult{
				TowKod: item.TowKod,
				Title:  item.NameB2B,
				Error:  publishErr.Error(),
			})
			continue
		}

		publishedTowKods = append(publishedTowKods, item.TowKod)
		log.Printf("[PUBLISH] success towKod=%s listingID=%d", item.TowKod, listingID)
		result.Published = append(result.Published, PublishedListingResult{
			TowKod:      item.TowKod,
			Title:       item.NameB2B,
			ListingID:   listingID,
			Published:   true,
			PublishedAt: time.Now().Format(time.RFC3339),
			Message:     "objavljen na OLX",
		})
	}

	if len(publishedTowKods) > 0 {
		if err := a.store.DeleteItemsBulk(publishedTowKods); err != nil {
			return CatalogPublishResponse{}, http.StatusInternalServerError, fmt.Errorf("objava je uspela ali čišćenje temp artikala nije uspelo: %w", err)
		}
	}

	result.State = a.store.State()
	result.Message = buildPublishSummaryMessage(len(result.Published), len(result.Skipped), len(result.Failed), useSelection)

	if len(result.Published) == 0 && len(result.Failed) == 0 && len(result.Skipped) == 0 {
		return CatalogPublishResponse{}, http.StatusBadRequest, fmt.Errorf("nema artikala za objavu")
	}

	return result, http.StatusOK, nil
}

func (a *App) publishSingleItem(ctx context.Context, item CatalogItem, document DescriptionDocument) (int, error) {
	if item.Categorization == nil || item.Categorization.Category == nil {
		return 0, fmt.Errorf("kategorija nije podešena")
	}

	listingPayload, err := a.buildListingPayload(ctx, item, document)
	if err != nil {
		return 0, err
	}

	payloadPreview, _ := json.Marshal(listingPayload)
	log.Printf("[PUBLISH] create listing towKod=%s payload=%s", item.TowKod, string(payloadPreview))

	listingID, err := a.olxCategories.CreateListing(ctx, listingPayload)
	if err != nil {
		return 0, err
	}

	log.Printf("[PUBLISH] draft created towKod=%s listingID=%d", item.TowKod, listingID)

	mainImageID, err := a.uploadListingImages(ctx, listingID, item.Images)
	if err != nil {
		return 0, err
	}

	log.Printf("[PUBLISH] images uploaded towKod=%s listingID=%d mainImageID=%d", item.TowKod, listingID, mainImageID)

	if mainImageID > 0 {
		if err := a.olxCategories.SetListingMainImage(ctx, listingID, mainImageID); err != nil {
			log.Printf(
				"[PUBLISH] warning towKod=%s listingID=%d mainImageID=%d set-main failed, continuing: %v",
				item.TowKod,
				listingID,
				mainImageID,
				err,
			)
		} else {
			log.Printf("[PUBLISH] main image set towKod=%s listingID=%d imageID=%d", item.TowKod, listingID, mainImageID)
		}
	}

	if err := a.olxCategories.PublishListing(ctx, listingID); err != nil {
		return 0, err
	}

	log.Printf("[PUBLISH] listing published towKod=%s listingID=%d", item.TowKod, listingID)

	return listingID, nil
}

func (a *App) buildListingPayload(ctx context.Context, item CatalogItem, document DescriptionDocument) (olxCreateListingRequest, error) {
	categoryID := item.Categorization.Category.ID
	attributes, err := a.olxCategories.GetCategoryAttributes(ctx, categoryID)
	if err != nil {
		return olxCreateListingRequest{}, fmt.Errorf("nije moguće učitati atribute za kategoriju %d: %w", categoryID, err)
	}

	title := buildListingTitle(item)
	description := buildListingDescription(item, renderDescriptionTemplate(document.Content, item))

	payload := olxCreateListingRequest{
		Title:            title,
		ShortDescription: buildShortDescription(item),
		Description:      description,
		Price:            item.RetailPrice,
		Available:        true,
		ListingType:      "sell",
		State:            "new",
		SKUNumber:        firstNonEmpty(strings.TrimSpace(item.Index), strings.TrimSpace(item.TowKod)),
		CategoryID:       strconv.Itoa(categoryID),
		Attributes:       mapOLXAttributeValues(attributes, item.Categorization.Attributes),
	}

	if item.Categorization.Brand != nil && item.Categorization.Brand.ID > 0 {
		payload.BrandID = strconv.Itoa(item.Categorization.Brand.ID)
	}

	if item.Categorization.Model != nil && item.Categorization.Model.ID > 0 {
		payload.ModelID = strconv.Itoa(item.Categorization.Model.ID)
	}

	return payload, nil
}

func (a *App) uploadListingImages(ctx context.Context, listingID int, images []CatalogImage) (int, error) {
	sortedImages := make([]CatalogImage, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.URL) == "" {
			continue
		}
		sortedImages = append(sortedImages, image)
	}

	sort.SliceStable(sortedImages, func(left int, right int) bool {
		return sortedImages[left].Position < sortedImages[right].Position
	})

	if len(sortedImages) == 0 {
		return 0, nil
	}

	log.Printf("[PUBLISH] upload images listingID=%d imageURLs=%v", listingID, extractImageURLs(sortedImages))

	uploadedImages, err := a.olxCategories.UploadListingImages(ctx, listingID, sortedImages)
	if err != nil {
		return 0, err
	}

	if len(uploadedImages) == 0 {
		return 0, nil
	}

	return uploadedImages[0].ID, nil
}

func extractImageURLs(images []CatalogImage) []string {
	urls := make([]string, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.URL) == "" {
			continue
		}
		urls = append(urls, image.URL)
	}
	return urls
}

func buildListingTitle(item CatalogItem) string {
	parts := compactUniqueStrings(
		item.NameB2B,
		item.BrandName,
		item.Index,
	)
	title := strings.Join(parts, " | ")
	if strings.TrimSpace(title) == "" {
		title = firstNonEmpty(item.TowKod, "Artikal")
	}

	if len(title) > 120 {
		return strings.TrimSpace(title[:120])
	}

	return title
}

func buildShortDescription(item CatalogItem) string {
	parts := compactUniqueStrings(
		item.NameB2B,
		item.BrandName,
		item.Index,
		item.TowKod,
	)
	shortDescription := strings.Join(parts, " | ")
	if len(shortDescription) > 180 {
		return strings.TrimSpace(shortDescription[:180])
	}
	return shortDescription
}

func buildListingDescription(item CatalogItem, renderedDescription string) string {
	lines := []string{
		buildDescriptionLine("Naziv", item.NameB2B),
		buildDescriptionLine("Brend", item.BrandName),
		buildDescriptionLine("Sifra", item.Index),
		buildDescriptionLine("TowKod", item.TowKod),
	}

	if item.Categorization != nil && item.Categorization.Category != nil {
		lines = append(lines, buildDescriptionLine("Kategorija", item.Categorization.Category.Name))
	}

	if item.RetailPrice > 0 {
		lines = append(lines, buildDescriptionLine("Cijena", fmt.Sprintf("%s %s", formatTemplateNumber(item.RetailPrice), formatTemplateCurrency(item.CurrencyCode))))
	}

	if strings.TrimSpace(item.TechnicalDescription) != "" {
		lines = append(lines, "", "Tehnicki opis:", item.TechnicalDescription)
	}

	if strings.TrimSpace(renderedDescription) != "" {
		lines = append(lines, "", "OLX opis:", renderedDescription)
	}

	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(filtered) == 0 || filtered[len(filtered)-1] == "" {
				continue
			}
			filtered = append(filtered, "")
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func buildDescriptionLine(label string, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("%s: %s", label, trimmed)
}

func buildPublishSummaryMessage(published int, skipped int, failed int, usedSelection bool) string {
	targetLabel := "kategorizovani artikli"
	if usedSelection {
		targetLabel = "selektovani artikli sa zelenom kategorizacijom"
	}

	return fmt.Sprintf(
		"Objava završena za %s. Objavljeno: %d, preskočeno: %d, grešaka: %d.",
		targetLabel,
		published,
		skipped,
		failed,
	)
}

func compactUniqueStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}

		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func mapOLXAttributeValues(attributes []OLXCategoryAttribute, values map[string]string) []olxListingAttributeValue {
	if len(attributes) == 0 || len(values) == 0 {
		return nil
	}

	lookup := make(map[string]OLXCategoryAttribute, len(attributes)*2)
	for _, attribute := range attributes {
		if trimmed := strings.TrimSpace(attribute.Name); trimmed != "" {
			lookup[strings.ToLower(trimmed)] = attribute
		}
		if trimmed := strings.TrimSpace(attribute.DisplayName); trimmed != "" {
			lookup[strings.ToLower(trimmed)] = attribute
		}
	}

	result := make([]olxListingAttributeValue, 0, len(values))
	added := map[int]struct{}{}
	for key, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}

		attribute, ok := lookup[strings.ToLower(strings.TrimSpace(key))]
		if !ok || attribute.ID == 0 {
			continue
		}
		if _, exists := added[attribute.ID]; exists {
			continue
		}

		added[attribute.ID] = struct{}{}
		result = append(result, olxListingAttributeValue{
			ID:    attribute.ID,
			Value: trimmedValue,
		})
	}

	sort.SliceStable(result, func(left int, right int) bool {
		return result[left].ID < result[right].ID
	})

	return result
}

func (s *OLXCategoryService) CreateListing(ctx context.Context, payload olxCreateListingRequest) (int, error) {
	body, err := s.doJSONRequest(ctx, http.MethodPost, olxBaseURL+"/listings", payload)
	if err != nil {
		return 0, fmt.Errorf("kreiranje oglasa nije uspelo: %w", err)
	}

	log.Printf("[OLX] create listing response=%s", strings.TrimSpace(string(body)))

	var direct olxListingResponse
	if err := json.Unmarshal(body, &direct); err == nil && direct.ID > 0 {
		return direct.ID, nil
	}

	var wrapped olxWrappedListingResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data.ID > 0 {
		return wrapped.Data.ID, nil
	}

	return 0, fmt.Errorf("OLX create listing odgovor ne sadrži id")
}

func (s *OLXCategoryService) UploadListingImages(ctx context.Context, listingID int, images []CatalogImage) ([]olxImageUploadResponse, error) {
	body, err := s.doMultipartImageUpload(ctx, listingID, images)
	if err != nil {
		return nil, fmt.Errorf("upload slika nije uspeo: %w", err)
	}

	log.Printf("[OLX] image upload response listingID=%d response=%s", listingID, strings.TrimSpace(string(body)))

	var direct []olxImageUploadResponse
	if err := json.Unmarshal(body, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}

	var wrapped olxWrappedImageUploadResponse
	if err := json.Unmarshal(body, &wrapped); err == nil {
		return wrapped.Data, nil
	}

	return nil, fmt.Errorf("OLX upload slika odgovor nije validan")
}

func (s *OLXCategoryService) SetListingMainImage(ctx context.Context, listingID int, imageID int) error {
	payload := map[string]string{
		"imageId": strconv.Itoa(imageID),
	}

	_, putErr := s.doJSONRequest(ctx, http.MethodPut, fmt.Sprintf("%s/listings/%d/image-main", olxBaseURL, listingID), payload)
	if putErr == nil {
		log.Printf("[OLX] main image set via PUT listingID=%d imageID=%d", listingID, imageID)
		return nil
	}

	log.Printf("[OLX] main image PUT failed listingID=%d imageID=%d error=%v", listingID, imageID, putErr)

	_, postErr := s.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/listings/%d/image-main", olxBaseURL, listingID), payload)
	if postErr == nil {
		log.Printf("[OLX] main image set via POST listingID=%d imageID=%d", listingID, imageID)
		return nil
	}

	log.Printf("[OLX] main image POST failed listingID=%d imageID=%d error=%v", listingID, imageID, postErr)
	return fmt.Errorf("postavljanje glavne slike nije uspelo: put=%v; post=%v", putErr, postErr)
}

func (s *OLXCategoryService) PublishListing(ctx context.Context, listingID int) error {
	body, err := s.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/listings/%d/publish", olxBaseURL, listingID), nil)
	if err != nil {
		return fmt.Errorf("objava oglasa nije uspela: %w", err)
	}
	log.Printf("[OLX] publish response listingID=%d response=%s", listingID, strings.TrimSpace(string(body)))
	return nil
}

func (s *OLXCategoryService) doJSONRequest(ctx context.Context, method string, requestURL string, payload any) ([]byte, error) {
	if !s.HasToken() {
		return nil, fmt.Errorf("OLX token nije podešen. Zalijepi ga u backend/token.json")
	}

	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OLX request nije uspeo: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("OLX API greška (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func (s *OLXCategoryService) doMultipartImageUpload(ctx context.Context, listingID int, images []CatalogImage) ([]byte, error) {
	if !s.HasToken() {
		return nil, fmt.Errorf("OLX token nije podešen. Zalijepi ga u backend/token.json")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for index, image := range images {
		imageURL := strings.TrimSpace(image.URL)
		if imageURL == "" {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
		if err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("preuzimanje slike nije uspelo: %w", err)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("preuzimanje slike nije uspelo: %w", err)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			_ = writer.Close()
			return nil, fmt.Errorf("preuzimanje slike nije uspelo (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
		}

		fileName := imageFileNameFromURL(imageURL, index)
		part, err := writer.CreateFormFile("images[]", fileName)
		if err != nil {
			resp.Body.Close()
			_ = writer.Close()
			return nil, err
		}

		if _, err := io.Copy(part, resp.Body); err != nil {
			resp.Body.Close()
			_ = writer.Close()
			return nil, fmt.Errorf("kopiranje slike u multipart nije uspelo: %w", err)
		}
		resp.Body.Close()
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/listings/%d/image-upload", olxBaseURL, listingID),
		&body,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OLX request nije uspeo: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("OLX API greška (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return responseBody, nil
}

func imageFileNameFromURL(rawURL string, index int) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(parsed.Path)
		base = strings.TrimSpace(base)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	return fmt.Sprintf("image-%d.jpg", index)
}
