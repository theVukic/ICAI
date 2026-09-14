package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodeGeneratedDescriptionRepairsCommonModelJSON(t *testing.T) {
	content := `{
		"title": "Test proizvod",
		"intro": ["Prvi red
drugi red",],
		"features": [],
		"dimensions": [],
		"notes": [],
	}`

	generated, err := decodeGeneratedDescription(content)
	if err != nil {
		t.Fatalf("decodeGeneratedDescription() error = %v", err)
	}
	if generated.Title != "Test proizvod" {
		t.Fatalf("title = %q", generated.Title)
	}
	if len(generated.Intro) != 1 || generated.Intro[0] != "Prvi red\ndrugi red" {
		t.Fatalf("intro = %#v", generated.Intro)
	}
}

func TestRequestDescriptionFallsBackAfterMalformedJSON(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format, _ := payload["response_format"].(map[string]any)
		if requests == 1 && format["type"] != "json_schema" {
			t.Fatalf("first response format = %#v", format)
		}
		if requests == 2 && format["type"] != "json_object" {
			t.Fatalf("retry response format = %#v", format)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": `{"title":"Proizvod", ["pokvaren JSON"]}`},
			}},
		})
	}))
	defer server.Close()

	processor := NewDescriptionProcessor(t.TempDir(), t.TempDir())
	processor.client = server.Client()
	item := CatalogItem{NameB2B: "Krovni nosač", TowKod: "RB-004-1", ItemID: "test-item"}
	generated, err := processor.requestDescription(context.Background(), server.URL, item, []descriptionEvidence{{ID: "catalog", Text: "Čelični krovni nosač."}})
	if err != nil {
		t.Fatalf("requestDescription() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if generated.Title != item.NameB2B {
		t.Fatalf("title = %q", generated.Title)
	}
	if len(generated.Features) != 1 || generated.Features[0].Text != "Čelični krovni nosač." {
		t.Fatalf("features = %#v", generated.Features)
	}
}

func TestRequestDescriptionRetriesWithoutSchemaWhenUnsupported(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": `{"title":"Krovni nosač","intro":[],"features":[],"dimensions":[],"notes":[]}`},
			}},
		})
	}))
	defer server.Close()

	processor := NewDescriptionProcessor(t.TempDir(), t.TempDir())
	processor.client = server.Client()
	generated, err := processor.requestDescription(context.Background(), server.URL, CatalogItem{NameB2B: "Krovni nosač"}, []descriptionEvidence{{ID: "catalog", Text: "Opis"}})
	if err != nil {
		t.Fatalf("requestDescription() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if generated.Title != "Krovni nosač" {
		t.Fatalf("title = %q", generated.Title)
	}
}
