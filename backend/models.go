package main

import "time"

type UploadedCatalog struct {
	ID         string        `json:"id"`
	FileName   string        `json:"fileName"`
	SavedPath  string        `json:"savedPath"`
	UploadedAt time.Time     `json:"uploadedAt"`
	TotalItems int           `json:"totalItems"`
	Items      []CatalogItem `json:"items"`
}

type CatalogPayload struct {
	GeneratedAt string        `json:"generatedAt"`
	TimeZone    string        `json:"timeZone"`
	TotalItems  int           `json:"totalItems"`
	Items       []CatalogItem `json:"items"`
}

type CatalogItem struct {
	SelectionKey         string                 `json:"selectionKey,omitempty"`
	TowKod               string                 `json:"towKod"`
	Index                string                 `json:"index"`
	NameB2B              string                 `json:"nameB2B"`
	BrandName            string                 `json:"brandName"`
	Images               []CatalogImage         `json:"images"`
	TechnicalDescription string                 `json:"technicalDescription"`
	Status               CatalogStatus          `json:"status"`
	RetailPrice          float64                `json:"retailPrice"`
	CurrencyCode         string                 `json:"currencyCode"`
	SourceCatalogID      string                 `json:"sourceCatalogId,omitempty"`
	SourceFileName       string                 `json:"sourceFileName,omitempty"`
	Categorization       *ItemCategorySelection `json:"categorization,omitempty"`
}

type CatalogImage struct {
	Position int    `json:"position"`
	URL      string `json:"url"`
	Path     string `json:"path,omitempty"`
}

type CatalogStatus struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type CatalogState struct {
	Catalogs    []UploadedCatalog `json:"catalogs"`
	Items       []CatalogItem     `json:"items"`
	TempDir     string            `json:"tempDir"`
	TotalFiles  int               `json:"totalFiles"`
	TotalItems  int               `json:"totalItems"`
	LastUpdated *time.Time        `json:"lastUpdated"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CatalogItemEditRequest struct {
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

type CatalogBulkEditRequest struct {
	ItemKeys     []string `json:"itemKeys,omitempty"`
	TowKods      []string `json:"towKods,omitempty"`
	Name         *string  `json:"name,omitempty"`
	Price        *float64 `json:"price,omitempty"`
	Description  *string  `json:"description,omitempty"`
	CurrencyCode *string  `json:"currencyCode,omitempty"`
}

type CatalogBulkDeleteRequest struct {
	ItemKeys []string `json:"itemKeys,omitempty"`
	TowKods  []string `json:"towKods,omitempty"`
}

type ItemCategorySelection struct {
	Category   *OLXCategoryNode  `json:"category,omitempty"`
	Brand      *OLXBrand         `json:"brand,omitempty"`
	Model      *OLXModel         `json:"model,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type CategoryPageResponse struct {
	Item            CatalogItem            `json:"item"`
	Categorization  *ItemCategorySelection `json:"categorization,omitempty"`
	TokenConfigured bool                   `json:"tokenConfigured"`
}

type CategorySaveRequest struct {
	Category   *OLXCategoryNode  `json:"category"`
	Brand      *OLXBrand         `json:"brand,omitempty"`
	Model      *OLXModel         `json:"model,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type CategoryBulkSaveRequest struct {
	ItemKeys   []string          `json:"itemKeys,omitempty"`
	TowKods    []string          `json:"towKods,omitempty"`
	Category   *OLXCategoryNode  `json:"category"`
	Brand      *OLXBrand         `json:"brand,omitempty"`
	Model      *OLXModel         `json:"model,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type OLXCategoryNode struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	NameSingular      string `json:"name_singular,omitempty"`
	Slug              string `json:"slug,omitempty"`
	ParentID          *int   `json:"parent_id,omitempty"`
	ShowBrand         bool   `json:"show_brand,omitempty"`
	BrandRequired     bool   `json:"brand_required,omitempty"`
	ModelRequired     bool   `json:"model_required,omitempty"`
	HasModels         bool   `json:"has_models,omitempty"`
	ShowPrice         bool   `json:"show_price,omitempty"`
	ShowCondition     bool   `json:"show_condition,omitempty"`
	ShippingAvailable bool   `json:"shipping_available,omitempty"`
}

type OLXCategoryAttribute struct {
	ID          int      `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	InputType   string   `json:"input_type"`
	DisplayName string   `json:"display_name"`
	Options     []string `json:"options"`
	Required    bool     `json:"required"`
	Order       int      `json:"order"`
}

type OLXBrand struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

type OLXModel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

type OLXCategorySuggestion struct {
	ID               int      `json:"id"`
	Count            int      `json:"count,omitempty"`
	Name             string   `json:"name"`
	ParentCategories []string `json:"parent_categories,omitempty"`
	Path             string   `json:"path,omitempty"`
}

type SessionInfoResponse struct {
	TokenConfigured bool   `json:"tokenConfigured"`
	Username        string `json:"username"`
	FullName        string `json:"fullName"`
	ProfileName     string `json:"profileName,omitempty"`
	Authenticated   bool   `json:"authenticated"`
	AppUsername     string `json:"appUsername,omitempty"`
	AppRole         string `json:"appRole,omitempty"`
	AvailableTokens []string `json:"availableTokens,omitempty"`
}

type TokenSwitchRequest struct {
	Name string `json:"name"`
}

type LoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type LoginResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	Role          string `json:"role,omitempty"`
	Message       string `json:"message,omitempty"`
}

type ListingLimitSegment struct {
	Segment   string `json:"segment"`
	Limit     int    `json:"limit"`
	Listings  int    `json:"listings"`
	Remaining int    `json:"remaining"`
}

type ListingLimitsResponse struct {
	TokenConfigured bool                  `json:"tokenConfigured"`
	Unlimited       bool                  `json:"unlimited"`
	Segment         string                `json:"segment"`
	Remaining       int                   `json:"remaining"`
	TotalLimit      int                   `json:"totalLimit"`
	TotalListings   int                   `json:"totalListings"`
	CheckedAt       string                `json:"checkedAt,omitempty"`
	Segments        []ListingLimitSegment `json:"segments"`
}

type DescriptionDocument struct {
	ProfileName string     `json:"profileName"`
	Content     string     `json:"content"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

type DescriptionSaveRequest struct {
	Content string `json:"content"`
}

type DescriptionRenderResponse struct {
	ProfileName string `json:"profileName"`
	TowKod      string `json:"towKod"`
	Content     string `json:"content"`
}

type CatalogPublishRequest struct {
	SelectedItemKeys []string `json:"selectedItemKeys,omitempty"`
	SelectedTowKods []string `json:"selectedTowKods"`
}

type PublishedListingResult struct {
	TowKod      string `json:"towKod"`
	Title       string `json:"title"`
	ListingID   int    `json:"listingId"`
	Published   bool   `json:"published"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Message     string `json:"message,omitempty"`
}

type SkippedListingResult struct {
	TowKod string `json:"towKod"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type FailedListingResult struct {
	TowKod string `json:"towKod"`
	Title  string `json:"title"`
	Error  string `json:"error"`
}

type CatalogPublishResponse struct {
	Message       string                   `json:"message"`
	Published     []PublishedListingResult `json:"published"`
	Skipped       []SkippedListingResult   `json:"skipped"`
	Failed        []FailedListingResult    `json:"failed"`
	State         CatalogState             `json:"state"`
	ListingLimits *ListingLimitsResponse   `json:"listingLimits,omitempty"`
}
