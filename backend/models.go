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
	TowKod               string         `json:"towKod"`
	Index                string         `json:"index"`
	NameB2B              string         `json:"nameB2B"`
	BrandName            string         `json:"brandName"`
	Images               []CatalogImage `json:"images"`
	TechnicalDescription string         `json:"technicalDescription"`
	Status               CatalogStatus  `json:"status"`
	RetailPrice          float64        `json:"retailPrice"`
	CurrencyCode         string         `json:"currencyCode"`
	SourceCatalogID      string         `json:"sourceCatalogId,omitempty"`
	SourceFileName       string         `json:"sourceFileName,omitempty"`
}

type CatalogImage struct {
	Position int    `json:"position"`
	URL      string `json:"url"`
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
