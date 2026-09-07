package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minUploadAspectRatio   = 0.8
	blurRadius             = 18
	uploadJPEGQuality      = 92
	publishWorkerCount     = 2
	imageUploadAttempts    = 2
	createListingAttempts  = 2
	publishListingAttempts = 3
	listingLimitsAttempts  = 3
	retryBaseDelay         = 2 * time.Second
	largeBatchThreshold    = 20
)

type publishJob struct {
	index int
	item  CatalogItem
}

type publishOutcome struct {
	index     int
	item      CatalogItem
	listingID int
	err       error
}

type categoryAttributesCache struct {
	mu      sync.Mutex
	entries map[int]*categoryAttributesCacheEntry
}

type categoryAttributesCacheEntry struct {
	ready      chan struct{}
	attributes []OLXCategoryAttribute
	err        error
}

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
	if pending, failed := a.store.AIWorkBlocking(req.SelectedItemKeys, req.SelectedTowKods); pending > 0 || failed > 0 {
		message := fmt.Sprintf("sačekajte da AI završi opise (%d preostalo)", pending)
		if failed > 0 {
			message = fmt.Sprintf("AI opis nije uspio za %d artikala; ponovite obradu ili isključite AI", failed)
		}
		writeError(w, http.StatusConflict, message)
		return
	}

	log.Printf("[PUBLISH] request received selectedTowKods=%v", req.SelectedTowKods)

	reservedItems, _, err := a.store.ReserveItemsForPublish(req.SelectedItemKeys, req.SelectedTowKods)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	publishCtx, cancel := context.WithTimeout(r.Context(), 60*time.Minute)
	defer cancel()

	queueReq := queuedPublishRequest{
		ctx:        publishCtx,
		items:      reservedItems,
		responseCh: make(chan queuedPublishResult, 1),
	}

	select {
	case a.publishQueue <- queueReq:
		log.Printf("[PUBLISH] request queued towKods=%v", req.SelectedTowKods)
	case <-publishCtx.Done():
		a.store.ReleasePublishReservations(extractSelectionKeys(reservedItems))
		writeError(w, http.StatusRequestTimeout, fmt.Sprintf("publish zahtjev je otkazan prije ulaska u red: %v", publishCtx.Err()))
		return
	}

	var queuedResult queuedPublishResult
	select {
	case queuedResult = <-queueReq.responseCh:
	case <-publishCtx.Done():
		writeError(w, http.StatusRequestTimeout, fmt.Sprintf("publish zahtjev je otkazan dok je čekao red ili obradu: %v", publishCtx.Err()))
		return
	}

	if queuedResult.err != nil {
		log.Printf("[PUBLISH] request failed status=%d error=%v", queuedResult.status, queuedResult.err)
		writeError(w, queuedResult.status, queuedResult.err.Error())
		return
	}

	log.Printf(
		"[PUBLISH] completed published=%d skipped=%d failed=%d",
		len(queuedResult.response.Published),
		len(queuedResult.response.Skipped),
		len(queuedResult.response.Failed),
	)
	writeJSON(w, http.StatusOK, queuedResult.response)
}

func (a *App) publishCatalogItems(ctx context.Context, items []CatalogItem) (CatalogPublishResponse, int, error) {
	if len(items) == 0 {
		return CatalogPublishResponse{}, http.StatusBadRequest, fmt.Errorf("nema artikala za objavu")
	}
	// Items are reserved before they enter the single publish worker. Refresh
	// them at execution time so a description that finished while the request
	// was queued is the one sent to OLX, never the old supplier snapshot.
	itemKeys := extractSelectionKeys(items)
	if pending, failed := a.store.AIWorkBlocking(itemKeys, nil); pending > 0 || failed > 0 {
		if failed > 0 {
			return CatalogPublishResponse{}, http.StatusConflict, fmt.Errorf("AI opis nije uspio za %d artikala; ponovite obradu ili isključite AI", failed)
		}
		return CatalogPublishResponse{}, http.StatusConflict, fmt.Errorf("sačekajte da AI završi opise (%d preostalo)", pending)
	}
	for index, reservedItem := range items {
		currentItem, err := a.store.GetItemByID(reservedItem.ItemID)
		if err != nil {
			return CatalogPublishResponse{}, http.StatusConflict, fmt.Errorf("artikal više nije dostupan za objavu: %w", err)
		}
		currentItem.SelectionKey = reservedItem.SelectionKey
		currentItem.Categorization = reservedItem.Categorization
		items[index] = currentItem
	}
	useSelection := true
	log.Printf("[PUBLISH] processing queued batch size=%d", len(items))

	document, err := a.loadDescriptionDocument()
	if err != nil {
		return CatalogPublishResponse{}, http.StatusInternalServerError, err
	}

	result := CatalogPublishResponse{
		Published: []PublishedListingResult{},
		Skipped:   []SkippedListingResult{},
		Failed:    []FailedListingResult{},
	}

	// TowKod is not a unique identifier: a supplier can use the same code for
	// several fitments. Keep the per-catalog selection key throughout publishing
	// so a successful item cannot remove failed items with the same TowKod.
	publishedSelectionKeys := make([]string, 0)
	defer func() {
		a.store.ReleasePublishReservations(collectUnpublishedSelectionKeys(items, publishedSelectionKeys))
	}()
	publishableJobs := make([]publishJob, 0)

	for _, item := range items {
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
		if a.imageProcessingIsEnabled() {
			generatedPath, progress := a.store.ProcessedImage(item.ItemID)
			if progress.Status == "done" && strings.TrimSpace(generatedPath) != "" {
				// Position -1 guarantees the generated cover is uploaded first while
				// retaining the imported first image as the second OLX image.
				item.Images = append([]CatalogImage{{Position: -1, Path: generatedPath}}, item.Images...)
			}
		}

		publishableJobs = append(publishableJobs, publishJob{
			index: len(publishableJobs),
			item:  item,
		})
	}

	if len(publishableJobs) > 0 {
		attributeCache := newCategoryAttributesCache()
		outcomes := a.publishItemsParallel(ctx, publishableJobs, document, attributeCache)

		for _, outcome := range outcomes {
			if outcome.err != nil {
				log.Printf("[PUBLISH] failed towKod=%s error=%v", outcome.item.TowKod, outcome.err)
				result.Failed = append(result.Failed, FailedListingResult{
					TowKod: outcome.item.TowKod,
					Title:  outcome.item.NameB2B,
					Error:  outcome.err.Error(),
				})
				continue
			}

			publishedSelectionKeys = append(publishedSelectionKeys, outcome.item.SelectionKey)
			log.Printf("[PUBLISH] success towKod=%s listingID=%d", outcome.item.TowKod, outcome.listingID)
			result.Published = append(result.Published, PublishedListingResult{
				TowKod:      outcome.item.TowKod,
				Title:       outcome.item.NameB2B,
				ListingID:   outcome.listingID,
				Published:   true,
				PublishedAt: time.Now().Format(time.RFC3339),
				Message:     "objavljen na OLX",
			})
		}
	}

	if len(publishedSelectionKeys) > 0 {
		if err := a.store.DeleteItemsBulk(publishedSelectionKeys, nil); err != nil {
			return CatalogPublishResponse{}, http.StatusInternalServerError, fmt.Errorf("objava je uspela ali čišćenje temp artikala nije uspelo: %w", err)
		}
	}
	result.State = a.store.State()
	result.Message = buildPublishSummaryMessage(len(result.Published), len(result.Skipped), len(result.Failed), useSelection)

	if a.olxCategories != nil && a.olxCategories.HasToken() {
		limits, limitsErr := a.olxCategories.GetListingLimitsWithRetry(ctx)
		if limitsErr != nil {
			log.Printf("[PUBLISH] listing limits refresh failed: %v", limitsErr)
		} else {
			result.ListingLimits = &limits
		}
	}

	if len(result.Published) == 0 && len(result.Failed) == 0 && len(result.Skipped) == 0 {
		return CatalogPublishResponse{}, http.StatusBadRequest, fmt.Errorf("nema artikala za objavu")
	}

	return result, http.StatusOK, nil
}

func extractSelectionKeys(items []CatalogItem) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SelectionKey) == "" {
			continue
		}
		values = append(values, item.SelectionKey)
	}
	return values
}

func collectUnpublishedSelectionKeys(items []CatalogItem, publishedSelectionKeys []string) []string {
	publishedSet := make(map[string]struct{}, len(publishedSelectionKeys))
	for _, selectionKey := range publishedSelectionKeys {
		publishedSet[strings.TrimSpace(selectionKey)] = struct{}{}
	}

	remaining := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := publishedSet[strings.TrimSpace(item.SelectionKey)]; ok {
			continue
		}
		remaining = append(remaining, item.SelectionKey)
	}

	return remaining
}

func (a *App) publishItemsParallel(
	ctx context.Context,
	jobs []publishJob,
	document DescriptionDocument,
	attributeCache *categoryAttributesCache,
) []publishOutcome {
	workerCount := effectivePublishWorkerCount(len(jobs))
	if len(jobs) < workerCount {
		workerCount = len(jobs)
	}
	if workerCount <= 0 {
		return nil
	}

	jobCh := make(chan publishJob)
	outcomeCh := make(chan publishOutcome, len(jobs))
	var wg sync.WaitGroup

	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for job := range jobCh {
				listingID, err := a.publishSingleItem(ctx, job.item, document, attributeCache)
				outcomeCh <- publishOutcome{
					index:     job.index,
					item:      job.item,
					listingID: listingID,
					err:       err,
				}
			}
		}()
	}

	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)

	wg.Wait()
	close(outcomeCh)

	outcomes := make([]publishOutcome, len(jobs))
	for outcome := range outcomeCh {
		outcomes[outcome.index] = outcome
	}

	return outcomes
}

func (a *App) publishSingleItem(
	ctx context.Context,
	item CatalogItem,
	document DescriptionDocument,
	attributeCache *categoryAttributesCache,
) (int, error) {
	if item.Categorization == nil || item.Categorization.Category == nil {
		return 0, fmt.Errorf("kategorija nije podešena")
	}

	listingPayload, err := a.buildListingPayload(ctx, item, document, attributeCache)
	if err != nil {
		return 0, err
	}

	payloadPreview, _ := json.Marshal(listingPayload)
	log.Printf("[PUBLISH] create listing towKod=%s payload=%s", item.TowKod, string(payloadPreview))

	listingID, err := a.olxCategories.CreateListingWithRetry(ctx, item.TowKod, listingPayload)
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

	if err := a.olxCategories.PublishListingWithRetry(ctx, item.TowKod, listingID); err != nil {
		return 0, err
	}

	log.Printf("[PUBLISH] listing published towKod=%s listingID=%d", item.TowKod, listingID)

	return listingID, nil
}

func (a *App) buildListingPayload(
	ctx context.Context,
	item CatalogItem,
	document DescriptionDocument,
	attributeCache *categoryAttributesCache,
) (olxCreateListingRequest, error) {
	categoryID := item.Categorization.Category.ID
	attributes, err := attributeCache.Get(ctx, a.olxCategories, categoryID)
	if err != nil {
		return olxCreateListingRequest{}, fmt.Errorf("nije moguće učitati atribute za kategoriju %d: %w", categoryID, err)
	}

	title := buildListingTitle(item)
	description := buildListingDescription(item, renderDescriptionTemplate(document.Content, item))

	mappedAttributes, err := validateAndMapOLXAttributeValues(attributes, item.Categorization.Attributes)
	if err != nil {
		return olxCreateListingRequest{}, fmt.Errorf("neispravni atributi za kategoriju %d: %w", categoryID, err)
	}

	payload := olxCreateListingRequest{
		Title:            title,
		ShortDescription: buildShortDescription(item),
		Description:      description,
		Price:            item.RetailPrice,
		Available:        true,
		ListingType:      "sell",
		State:            "new",
		CategoryID:       strconv.Itoa(categoryID),
		Attributes:       mappedAttributes,
	}
	payload.SKUNumber = skuNumberForListing(item, a.olxCategories.IsShopAccount())

	if item.Categorization.Brand != nil && item.Categorization.Brand.ID > 0 {
		payload.BrandID = strconv.Itoa(item.Categorization.Brand.ID)
	}

	if item.Categorization.Model != nil && item.Categorization.Model.ID > 0 {
		payload.ModelID = strconv.Itoa(item.Categorization.Model.ID)
	}

	return payload, nil
}

func skuNumberForListing(item CatalogItem, isShop bool) string {
	if !isShop {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(item.Index), strings.TrimSpace(item.TowKod))
}

func newCategoryAttributesCache() *categoryAttributesCache {
	return &categoryAttributesCache{
		entries: make(map[int]*categoryAttributesCacheEntry),
	}
}

func (c *categoryAttributesCache) Get(
	ctx context.Context,
	service *OLXCategoryService,
	categoryID int,
) ([]OLXCategoryAttribute, error) {
	c.mu.Lock()
	if entry, ok := c.entries[categoryID]; ok {
		c.mu.Unlock()
		select {
		case <-entry.ready:
			return entry.attributes, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	entry := &categoryAttributesCacheEntry{
		ready: make(chan struct{}),
	}
	c.entries[categoryID] = entry
	c.mu.Unlock()

	entry.attributes, entry.err = service.GetCategoryAttributes(ctx, categoryID)
	close(entry.ready)

	return entry.attributes, entry.err
}

func (a *App) uploadListingImages(ctx context.Context, listingID int, images []CatalogImage) (int, error) {
	sortedImages := make([]CatalogImage, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.URL) == "" && strings.TrimSpace(image.Path) == "" {
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

func effectivePublishWorkerCount(jobCount int) int {
	if jobCount >= largeBatchThreshold {
		return 1
	}
	return publishWorkerCount
}

func buildListingTitle(item CatalogItem) string {
	title := strings.TrimSpace(item.NameB2B)
	if strings.TrimSpace(title) == "" {
		title = firstNonEmpty(item.TowKod, "Artikal")
	}

	if len(title) > 120 {
		return strings.TrimSpace(title[:120])
	}

	return title
}

func buildShortDescription(item CatalogItem) string {
	shortDescription := strings.TrimSpace(item.NameB2B)
	if shortDescription == "" {
		shortDescription = firstNonEmpty(strings.TrimSpace(item.TowKod), "Artikal")
	}
	if len(shortDescription) > 180 {
		return strings.TrimSpace(shortDescription[:180])
	}
	return shortDescription
}

func buildListingDescription(item CatalogItem, renderedDescription string) string {
	if strings.TrimSpace(renderedDescription) != "" {
		return strings.TrimSpace(renderedDescription)
	}

	return strings.TrimSpace(item.TechnicalDescription)
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
	result, _ := validateAndMapOLXAttributeValues(attributes, values)
	return result
}

// validateAndMapOLXAttributeValues keeps only attributes declared by the selected
// OLX category and prevents a request that OLX will reject with a generic 422.
func validateAndMapOLXAttributeValues(attributes []OLXCategoryAttribute, values map[string]string) ([]olxListingAttributeValue, error) {
	if len(attributes) == 0 || len(values) == 0 {
		for _, attribute := range attributes {
			if attribute.Required {
				return nil, fmt.Errorf("obavezni atribut %q nije izabran", attributeLabel(attribute))
			}
		}
		return nil, nil
	}

	lookup := make(map[string]OLXCategoryAttribute, len(attributes)*2)
	lookupByID := make(map[int]OLXCategoryAttribute, len(attributes))
	for _, attribute := range attributes {
		lookupByID[attribute.ID] = attribute
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
		if !ok {
			if attributeID, parseErr := strconv.Atoi(strings.TrimSpace(key)); parseErr == nil {
				attribute, ok = lookupByID[attributeID]
			}
		}
		if !ok || attribute.ID == 0 {
			continue
		}
		if _, exists := added[attribute.ID]; exists {
			continue
		}
		if len(attribute.Options) > 0 && !isOLXAttributeOption(attribute.Options, trimmedValue) {
			return nil, fmt.Errorf("atribut %q ima nedozvoljenu vrijednost %q (dozvoljeno: %s)", attributeLabel(attribute), trimmedValue, strings.Join(attribute.Options, ", "))
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

	for _, attribute := range attributes {
		if attribute.Required {
			if _, ok := added[attribute.ID]; !ok {
				return nil, fmt.Errorf("obavezni atribut %q nije izabran", attributeLabel(attribute))
			}
		}
	}

	return result, nil
}

func attributeLabel(attribute OLXCategoryAttribute) string {
	if displayName := strings.TrimSpace(attribute.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(attribute.Name)
}

func isOLXAttributeOption(options []string, value string) bool {
	for _, option := range options {
		if strings.TrimSpace(option) == value {
			return true
		}
	}
	return false
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

func (s *OLXCategoryService) CreateListingWithRetry(ctx context.Context, towKod string, payload olxCreateListingRequest) (int, error) {
	var listingID int
	var err error
	for attempt := 1; attempt <= createListingAttempts; attempt++ {
		listingID, err = s.CreateListing(ctx, payload)
		if err == nil {
			return listingID, nil
		}
		if !isRetriableOLXRequestError(err) || attempt == createListingAttempts {
			return 0, err
		}
		if waitErr := waitForRetry(ctx, attempt); waitErr != nil {
			return 0, err
		}
		log.Printf("[OLX] create listing retry towKod=%s attempt=%d/%d error=%v", towKod, attempt+1, createListingAttempts, err)
	}
	return 0, err
}

func (s *OLXCategoryService) UploadListingImages(ctx context.Context, listingID int, images []CatalogImage) ([]olxImageUploadResponse, error) {
	var body []byte
	var err error
	for attempt := 1; attempt <= imageUploadAttempts; attempt++ {
		body, err = s.doMultipartImageUpload(ctx, listingID, images)
		if err == nil {
			break
		}
		if !isRetriableOLXRequestError(err) || attempt == imageUploadAttempts {
			return nil, fmt.Errorf("upload slika nije uspeo: %w", err)
		}
		if waitErr := waitForRetry(ctx, attempt); waitErr != nil {
			return nil, fmt.Errorf("upload slika nije uspeo: %w", err)
		}
		log.Printf(
			"[OLX] image upload retry listingID=%d attempt=%d/%d error=%v",
			listingID,
			attempt+1,
			imageUploadAttempts,
			err,
		)
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

func (s *OLXCategoryService) PublishListingWithRetry(ctx context.Context, towKod string, listingID int) error {
	var err error
	for attempt := 1; attempt <= publishListingAttempts; attempt++ {
		err = s.PublishListing(ctx, listingID)
		if err == nil {
			return nil
		}
		if !isRetriableOLXRequestError(err) || attempt == publishListingAttempts {
			return err
		}
		if waitErr := waitForRetry(ctx, attempt); waitErr != nil {
			return err
		}
		log.Printf("[OLX] publish retry towKod=%s listingID=%d attempt=%d/%d error=%v", towKod, listingID, attempt+1, publishListingAttempts, err)
	}
	return err
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
		return nil, formatOLXAPIError(resp.StatusCode, body)
	}

	return body, nil
}

func (s *OLXCategoryService) doMultipartImageUpload(ctx context.Context, listingID int, images []CatalogImage) ([]byte, error) {
	if !s.HasToken() {
		return nil, fmt.Errorf("OLX token nije podešen. Zalijepi ga u backend/token.json")
	}

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	uploadErrCh := make(chan error, 1)

	go func() {
		uploadErrCh <- s.streamImagesToMultipart(ctx, writer, pipeWriter, images)
	}()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/listings/%d/image-upload", olxBaseURL, listingID),
		pipeReader,
	)
	if err != nil {
		_ = pipeReader.Close()
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.uploadHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OLX request nije uspeo: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, formatOLXAPIError(resp.StatusCode, responseBody)
	}

	if uploadErr := <-uploadErrCh; uploadErr != nil {
		return nil, uploadErr
	}

	return responseBody, nil
}

func (s *OLXCategoryService) GetListingLimitsWithRetry(ctx context.Context) (ListingLimitsResponse, error) {
	var limits ListingLimitsResponse
	var err error
	for attempt := 1; attempt <= listingLimitsAttempts; attempt++ {
		limits, err = s.GetListingLimits(ctx)
		if err == nil {
			return limits, nil
		}
		if !isRetriableOLXRequestError(err) || attempt == listingLimitsAttempts {
			return ListingLimitsResponse{}, err
		}
		if waitErr := waitForRetry(ctx, attempt); waitErr != nil {
			return ListingLimitsResponse{}, err
		}
		log.Printf("[OLX] listing limits retry attempt=%d/%d error=%v", attempt+1, listingLimitsAttempts, err)
	}
	return ListingLimitsResponse{}, err
}

func (s *OLXCategoryService) streamImagesToMultipart(
	ctx context.Context,
	writer *multipart.Writer,
	pipeWriter *io.PipeWriter,
	images []CatalogImage,
) error {
	closeWithError := func(err error) error {
		_ = pipeWriter.CloseWithError(err)
		return err
	}

	for index, image := range images {
		imageBytes, sourceContentType, imageSourceLabel, err := s.loadCatalogImageBytes(ctx, image)
		if err != nil {
			return closeWithError(err)
		}
		if len(imageBytes) == 0 {
			continue
		}

		uploadBytes, uploadContentType, transformed, err := prepareImageForUpload(imageBytes, sourceContentType)
		if err != nil {
			return closeWithError(fmt.Errorf("obrada slike nije uspela: %w", err))
		}

		if transformed {
			log.Printf("[PUBLISH] image padded with blurred sides source=%s", imageSourceLabel)
		}

		fileName := imageFileName(image, uploadContentType, index)
		part, err := createImageMultipartPart(writer, "images[]", fileName, uploadContentType)
		if err != nil {
			return closeWithError(err)
		}

		if _, err := io.Copy(part, bytes.NewReader(uploadBytes)); err != nil {
			return closeWithError(fmt.Errorf("kopiranje slike u multipart nije uspelo: %w", err))
		}
	}

	if err := writer.Close(); err != nil {
		return closeWithError(err)
	}

	return pipeWriter.Close()
}

func (s *OLXCategoryService) loadCatalogImageBytes(ctx context.Context, image CatalogImage) ([]byte, string, string, error) {
	imagePath := strings.TrimSpace(image.Path)
	if imagePath != "" {
		if imageBytes, contentType, err := loadImageBytesFromPath(imagePath); err == nil {
			return imageBytes, contentType, imagePath, nil
		} else if !os.IsNotExist(err) {
			return nil, "", imagePath, fmt.Errorf("čitanje lokalne slike nije uspelo: %w", err)
		}
	}

	imageURL := strings.TrimSpace(image.URL)
	if imageURL == "" {
		return nil, "", "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", imageURL, fmt.Errorf("preuzimanje slike nije uspelo: %w", err)
	}

	resp, err := s.imageHTTPClient.Do(req)
	if err != nil {
		return nil, "", imageURL, fmt.Errorf("preuzimanje slike nije uspelo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, "", imageURL, fmt.Errorf("preuzimanje slike nije uspelo (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	imageBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", imageURL, fmt.Errorf("čitanje slike nije uspelo: %w", err)
	}

	return imageBytes, resp.Header.Get("Content-Type"), imageURL, nil
}

func loadImageBytesFromPath(imagePath string) ([]byte, string, error) {
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, "", err
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(imagePath)))
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(imageBytes)
	}

	return imageBytes, contentType, nil
}

func createImageMultipartPart(writer *multipart.Writer, fieldName string, fileName string, contentType string) (io.Writer, error) {
	headers := make(textproto.MIMEHeader)
	headers.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, escapeMultipartQuotes(fileName)))

	trimmedContentType := strings.TrimSpace(contentType)
	if trimmedContentType == "" {
		trimmedContentType = "application/octet-stream"
	}
	headers.Set("Content-Type", trimmedContentType)

	return writer.CreatePart(headers)
}

func prepareImageForUpload(imageBytes []byte, contentType string) ([]byte, string, bool, error) {
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return imageBytes, firstNonEmpty(strings.TrimSpace(contentType), "image/jpeg"), false, nil
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return imageBytes, firstNonEmpty(strings.TrimSpace(contentType), "image/jpeg"), false, nil
	}

	aspectRatio := float64(width) / float64(height)
	if aspectRatio >= minUploadAspectRatio {
		return imageBytes, firstNonEmpty(strings.TrimSpace(contentType), "image/jpeg"), false, nil
	}

	targetWidth := int(math.Ceil(float64(height) * minUploadAspectRatio))
	if targetWidth <= width {
		return imageBytes, firstNonEmpty(strings.TrimSpace(contentType), "image/jpeg"), false, nil
	}

	background := resizeToCover(img, targetWidth, height)
	blurred := blurRGBA(background, blurRadius)
	canvas := image.NewRGBA(image.Rect(0, 0, targetWidth, height))
	draw.Draw(canvas, canvas.Bounds(), blurred, image.Point{}, draw.Src)

	offsetX := (targetWidth - width) / 2
	draw.Draw(
		canvas,
		image.Rect(offsetX, 0, offsetX+width, height),
		img,
		bounds.Min,
		draw.Over,
	)

	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: uploadJPEGQuality}); err != nil {
		return nil, "", false, err
	}

	return output.Bytes(), "image/jpeg", true, nil
}

func resizeToCover(src image.Image, targetWidth int, targetHeight int) *image.RGBA {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	scale := math.Max(float64(targetWidth)/float64(srcWidth), float64(targetHeight)/float64(srcHeight))
	scaledWidth := maxInt(1, int(math.Ceil(float64(srcWidth)*scale)))
	scaledHeight := maxInt(1, int(math.Ceil(float64(srcHeight)*scale)))

	scaled := resizeImageNearest(src, scaledWidth, scaledHeight)
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	offsetX := (scaledWidth - targetWidth) / 2
	offsetY := (scaledHeight - targetHeight) / 2
	draw.Draw(dst, dst.Bounds(), scaled, image.Point{X: offsetX, Y: offsetY}, draw.Src)

	return dst
}

func resizeImageNearest(src image.Image, targetWidth int, targetHeight int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	for y := 0; y < targetHeight; y++ {
		srcY := srcBounds.Min.Y + (y*srcHeight)/targetHeight
		for x := 0; x < targetWidth; x++ {
			srcX := srcBounds.Min.X + (x*srcWidth)/targetWidth
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	return dst
}

func blurRGBA(src *image.RGBA, radius int) *image.RGBA {
	if radius <= 0 {
		dst := image.NewRGBA(src.Bounds())
		copy(dst.Pix, src.Pix)
		return dst
	}

	bounds := src.Bounds()
	tmp := image.NewRGBA(bounds)
	dst := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var rSum, gSum, bSum, aSum, count int
			for sampleX := x - radius; sampleX <= x+radius; sampleX++ {
				clampedX := clampInt(sampleX, bounds.Min.X, bounds.Max.X-1)
				offset := src.PixOffset(clampedX, y)
				rSum += int(src.Pix[offset])
				gSum += int(src.Pix[offset+1])
				bSum += int(src.Pix[offset+2])
				aSum += int(src.Pix[offset+3])
				count++
			}

			offset := tmp.PixOffset(x, y)
			tmp.Pix[offset] = uint8(rSum / count)
			tmp.Pix[offset+1] = uint8(gSum / count)
			tmp.Pix[offset+2] = uint8(bSum / count)
			tmp.Pix[offset+3] = uint8(aSum / count)
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var rSum, gSum, bSum, aSum, count int
			for sampleY := y - radius; sampleY <= y+radius; sampleY++ {
				clampedY := clampInt(sampleY, bounds.Min.Y, bounds.Max.Y-1)
				offset := tmp.PixOffset(x, clampedY)
				rSum += int(tmp.Pix[offset])
				gSum += int(tmp.Pix[offset+1])
				bSum += int(tmp.Pix[offset+2])
				aSum += int(tmp.Pix[offset+3])
				count++
			}

			offset := dst.PixOffset(x, y)
			dst.Pix[offset] = uint8(rSum / count)
			dst.Pix[offset+1] = uint8(gSum / count)
			dst.Pix[offset+2] = uint8(bSum / count)
			dst.Pix[offset+3] = uint8(aSum / count)
		}
	}

	return dst
}

func imageFileNameFromURL(rawURL string, contentType string, index int) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(parsed.Path)
		base = strings.TrimSpace(base)
		if base != "" && base != "." && base != "/" {
			if path.Ext(base) == "" {
				if extension := extensionFromContentType(contentType); extension != "" {
					return base + extension
				}
			}
			return base
		}
	}

	if extension := extensionFromContentType(contentType); extension != "" {
		return fmt.Sprintf("image-%d%s", index, extension)
	}

	return fmt.Sprintf("image-%d.jpg", index)
}

func imageFileName(image CatalogImage, contentType string, index int) string {
	imagePath := strings.TrimSpace(image.Path)
	if imagePath != "" {
		base := strings.TrimSpace(filepath.Base(imagePath))
		if base != "" && base != "." && base != string(os.PathSeparator) {
			if filepath.Ext(base) == "" {
				if extension := extensionFromContentType(contentType); extension != "" {
					return base + extension
				}
			}
			return base
		}
	}

	return imageFileNameFromURL(image.URL, contentType, index)
}

func extensionFromContentType(contentType string) string {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		mediaType = trimmed
	}

	extensions, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(extensions) == 0 {
		return ""
	}

	return extensions[0]
}

func escapeMultipartQuotes(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func isRetriableOLXRequestError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return true
		}
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded")
}

func waitForRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt) * retryBaseDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
