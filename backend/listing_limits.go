package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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

	segments := make([]ListingLimitSegment, 0, len(wrapped.Data))
	totalLimit := 0
	totalListings := 0
	remaining := 0

	for segment, stats := range wrapped.Data {
		segmentRemaining := stats.Limit - stats.Listings
		if segmentRemaining < 0 {
			segmentRemaining = 0
		}

		totalLimit += stats.Limit
		totalListings += stats.Listings
		remaining += segmentRemaining

		segments = append(segments, ListingLimitSegment{
			Segment:   segment,
			Limit:     stats.Limit,
			Listings:  stats.Listings,
			Remaining: segmentRemaining,
		})
	}

	sort.SliceStable(segments, func(left int, right int) bool {
		return segments[left].Segment < segments[right].Segment
	})

	return ListingLimitsResponse{
		TokenConfigured: true,
		Remaining:       remaining,
		TotalLimit:      totalLimit,
		TotalListings:   totalListings,
		CheckedAt:       time.Now().Format(time.RFC3339),
		Segments:        segments,
	}, nil
}
