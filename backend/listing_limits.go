package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type olxListingLimitStats struct {
	Limit    int `json:"limit"`
	Listings int `json:"listings"`
}

type olxWrappedListingLimitsResponse struct {
	Data map[string]olxListingLimitStats `json:"data"`
}

func (a *App) handleListingLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.olxCategories == nil || !a.olxCategories.HasToken() {
		writeJSON(w, http.StatusOK, ListingLimitsResponse{
			TokenConfigured: false,
			Segments:        []ListingLimitSegment{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	limits, err := a.olxCategories.GetListingLimits(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	logEvent("olx", "listing limits loaded", map[string]any{
		"remaining":        limits.Remaining,
		"token_configured": limits.TokenConfigured,
		"total_limit":      limits.TotalLimit,
		"total_listings":   limits.TotalListings,
		"unlimited":        limits.Unlimited,
	})
	writeJSON(w, http.StatusOK, limits)
}

func (s *OLXCategoryService) GetListingLimits(ctx context.Context) (ListingLimitsResponse, error) {
	if !s.HasToken() {
		return ListingLimitsResponse{
			TokenConfigured: false,
			Segments:        []ListingLimitSegment{},
		}, nil
	}

	body, err := s.doJSONRequest(ctx, http.MethodGet, olxBaseURL+"/listing-limits", nil)
	if err != nil {
		return ListingLimitsResponse{}, fmt.Errorf("učitavanje listing limita nije uspelo: %w", err)
	}

	var wrapped olxWrappedListingLimitsResponse
	if err := json.Unmarshal(body, &wrapped); err != nil || len(wrapped.Data) == 0 {
		return ListingLimitsResponse{}, fmt.Errorf("OLX listing-limits odgovor nije validan")
	}

	stats, ok := wrapped.Data["other"]
	if !ok {
		return ListingLimitsResponse{}, fmt.Errorf("OLX listing-limits odgovor nema other segment")
	}

	remaining := stats.Limit - stats.Listings
	if remaining < 0 {
		remaining = 0
	}

	unlimited := stats.Limit == 0
	if unlimited {
		remaining = 0
	}

	segments := []ListingLimitSegment{
		{
			Segment:   "other",
			Limit:     stats.Limit,
			Listings:  stats.Listings,
			Remaining: remaining,
		},
	}

	return ListingLimitsResponse{
		TokenConfigured: true,
		Unlimited:       unlimited,
		Segment:         "other",
		Remaining:       remaining,
		TotalLimit:      stats.Limit,
		TotalListings:   stats.Listings,
		CheckedAt:       time.Now().Format(time.RFC3339),
		Segments:        segments,
	}, nil
}
