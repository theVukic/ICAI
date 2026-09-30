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
	// ItemID is generated when a catalog is imported and is the application's
	// stable identity for this row, independent of supplier fields.
	ItemID       string         `json:"itemId"`
	SelectionKey string         `json:"selectionKey,omitempty"`
	TowKod       string         `json:"towKod"`
	Index        string         `json:"index"`
	NameB2B      string         `json:"nameB2B"`
	BrandName    string         `json:"brandName"`
	Images       []CatalogImage `json:"images"`
	// ProcessedCover is written only into an exported ASI ZIP. It points to
	// the generated cover stored in that same ZIP, never to an app cache.
	ProcessedCover string `json:"processedCover,omitempty"`
	// ProcessedImageURL is a runtime-only authenticated URL for previewing the
	// generated image in the UI. It is never an exported cover path.
	ProcessedImageURL    string `json:"processedImageUrl,omitempty"`
	TechnicalDescription string `json:"technicalDescription"`
	// OriginalTechnicalDescription keeps the supplier text reversible when the
	// local AI writes rich HTML into TechnicalDescription.
	OriginalTechnicalDescription string `json:"originalTechnicalDescription,omitempty"`
	// AITechnicalDescription is kept separately so the user can instantly switch
	// between the supplier and generated version without losing either one.
	AITechnicalDescription     string                 `json:"aiTechnicalDescription,omitempty"`
	UseAIContent               bool                   `json:"useAIContent,omitempty"`
	TechnicalDescriptionFormat string                 `json:"technicalDescriptionFormat,omitempty"`
	Status                     CatalogStatus          `json:"status"`
	RetailPrice                float64                `json:"retailPrice"`
	CurrencyCode               string                 `json:"currencyCode"`
	SourceCatalogID            string                 `json:"sourceCatalogId,omitempty"`
	SourceFileName             string                 `json:"sourceFileName,omitempty"`
	Categorization             *ItemCategorySelection `json:"categorization,omitempty"`
}

type CatalogImage struct {
	Position int    `json:"position"`
	URL      string `json:"url"`
	Path     string `json:"path,omitempty"`
	ID       string `json:"id,omitempty"`
	Source   string `json:"source,omitempty"`
	Checksum string `json:"checksum,omitempty"`
	AIInput  bool   `json:"aiInput,omitempty"`
}

type CatalogStatus struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type CatalogState struct {
	Catalogs        []UploadedCatalog    `json:"catalogs"`
	Items           []CatalogItem        `json:"items"`
	TempDir         string               `json:"tempDir"`
	TotalFiles      int                  `json:"totalFiles"`
	TotalItems      int                  `json:"totalItems"`
	LastUpdated     *time.Time           `json:"lastUpdated"`
	ImageProcessing ImageProcessingState `json:"imageProcessing"`
	AIProcessing    AIProcessingState    `json:"aiProcessing"`
}

// ImageProcessingState is deliberately separate from CatalogItem. Generated
// images are runtime-only assets and must never alter the imported catalog.
type ImageProcessingState struct {
	Enabled bool                           `json:"enabled"`
	AIReady bool                           `json:"aiReady"`
	AIError string                         `json:"aiError,omitempty"`
	Items   map[string]ImageProcessingItem `json:"items"`
	Queued  int                            `json:"queued"`
	Working int                            `json:"working"`
	Done    int                            `json:"done"`
	Failed  int                            `json:"failed"`
}

type ImageProcessingItem struct {
	Status string `json:"status"` // queued, processing, done, error
	Error  string `json:"error,omitempty"`
	Model  string `json:"model,omitempty"` // fast, detailed
}

type DescriptionProcessingItem struct {
	Status string `json:"status"` // queued, processing, done, error
	Error  string `json:"error,omitempty"`
	Source string `json:"source,omitempty"` // web, catalog, cache
	Model  string `json:"model,omitempty"`
}

type AIProcessingItem struct {
	Image       ImageProcessingItem       `json:"image"`
	Description DescriptionProcessingItem `json:"description"`
}

type AIProcessingState struct {
	Enabled bool                        `json:"enabled"`
	AIReady bool                        `json:"aiReady"`
	AIError string                      `json:"aiError,omitempty"`
	Items   map[string]AIProcessingItem `json:"items"`
	Queued  int                         `json:"queued"`
	Working int                         `json:"working"`
	Done    int                         `json:"done"`
	Failed  int                         `json:"failed"`
}

type AIProcessRequest struct {
	ItemKeys []string `json:"itemKeys,omitempty"`
	TowKods  []string `json:"towKods,omitempty"`
	Kind     string   `json:"kind"` // image or description
}

type AIContentToggleRequest struct {
	Enabled bool `json:"enabled"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CatalogItemEditRequest struct {
	Name              string  `json:"name"`
	Price             float64 `json:"price"`
	Description       string  `json:"description"`
	DescriptionFormat string  `json:"descriptionFormat,omitempty"`
}

type CatalogBulkEditRequest struct {
	ItemKeys          []string `json:"itemKeys,omitempty"`
	TowKods           []string `json:"towKods,omitempty"`
	Name              *string  `json:"name,omitempty"`
	Price             *float64 `json:"price,omitempty"`
	Description       *string  `json:"description,omitempty"`
	DescriptionFormat *string  `json:"descriptionFormat,omitempty"`
	CurrencyCode      *string  `json:"currencyCode,omitempty"`
}

type CatalogBulkDeleteRequest struct {
	ItemKeys []string `json:"itemKeys,omitempty"`
	TowKods  []string `json:"towKods,omitempty"`
}

type CatalogExportRequest struct {
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
	TokenConfigured bool     `json:"tokenConfigured"`
	Username        string   `json:"username"`
	FullName        string   `json:"fullName"`
	ProfileName     string   `json:"profileName,omitempty"`
	UserType        string   `json:"userType,omitempty"`
	IsShop          bool     `json:"isShop"`
	Authenticated   bool     `json:"authenticated"`
	AppUsername     string   `json:"appUsername,omitempty"`
	AppRole         string   `json:"appRole,omitempty"`
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
	SelectedTowKods  []string `json:"selectedTowKods"`
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
