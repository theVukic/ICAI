package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSanitizeDescriptionHTMLKeepsOnlySupportedFormatting(t *testing.T) {
	input := `<h3 onclick="bad()">🚗 Naslov</h3><script>alert(1)</script><p><strong>Bitno</strong> <a href="https://bad">link</a></p><img src=x onerror=bad()>`
	got := sanitizeDescriptionHTML(input)
	if strings.Contains(got, "script") || strings.Contains(got, "onclick") || strings.Contains(got, "href") || strings.Contains(got, "<img") {
		t.Fatalf("unsafe markup survived sanitization: %s", got)
	}
	if !strings.Contains(got, "<h3>🚗 Naslov</h3>") || !strings.Contains(got, "<strong>Bitno</strong>") || !strings.Contains(got, "link") {
		t.Fatalf("supported content was lost: %s", got)
	}
}

func TestUnsupportedNumbersAndMaterialsAreRejected(t *testing.T) {
	sources := map[string]string{"catalog": normalizeEvidenceText("Niklovana dupla rora koja se pričvršćuje na vijke.")}
	claims := []generatedDescriptionClaim{
		{Text: "Odgovara cijevima do 58 mm", Evidence: []string{"catalog"}},
		{Text: "Izrađena od T-304 inox čelika", Evidence: []string{"catalog"}},
		{Text: "Niklovana završna obrada", Evidence: []string{"catalog"}},
	}
	got := validateClaims(claims, sources, 10)
	if len(got) != 1 || !strings.Contains(got[0].Text, "Niklovana") {
		t.Fatalf("expected only supported claim, got %#v", got)
	}
}

func TestProductEvidenceRequiresCodeAndNameMatch(t *testing.T) {
	item := CatalogItem{TowKod: "MAX 0949", NameB2B: "Rora niklovana dupla", BrandName: "Auto Stil"}
	if !productEvidenceMatches(item, "Auto Stil Rora niklovana dupla MAX 0949. Opis proizvoda.") {
		t.Fatal("expected exact product evidence to match")
	}
	if productEvidenceMatches(item, "Rora niklovana dupla MAX 0956") {
		t.Fatal("different product code must not match")
	}
}

func TestRenderedDescriptionUsesRichHTMLWithoutEvidenceLabels(t *testing.T) {
	generated := generatedDescription{
		Title:    "Dupli nastavak auspuha",
		Intro:    []string{"Moderan izgled za vaše vozilo."},
		Features: []generatedDescriptionClaim{{Text: "**Niklovana** završna obrada", Evidence: []string{"catalog"}}},
	}
	got := renderGeneratedDescription(generated, CatalogItem{NameB2B: "Rora"}, "Početni opis")
	if !strings.Contains(got, "<strong>Niklovana</strong>") || !strings.Contains(got, "✅ Karakteristike:") {
		t.Fatalf("expected rich description, got %s", got)
	}
	if strings.Contains(got, "catalog") {
		t.Fatalf("evidence labels must not be customer-visible: %s", got)
	}
}

func TestRenderedDescriptionRemovesSupplierIndexFromAIContent(t *testing.T) {
	generated := generatedDescription{
		Title:    "Rora niklovana dupla MAX 0949",
		Intro:    []string{"Model MAX 0949 daje uredan izgled."},
		Features: []generatedDescriptionClaim{{Text: "Niklovana rora 0949", Evidence: []string{"catalog"}}},
	}
	item := CatalogItem{NameB2B: "Rora MAX 0949", Index: "0949"}
	got := renderGeneratedDescription(generated, item, "Početni opis 0949")
	if strings.Contains(got, "0949") {
		t.Fatalf("supplier index must not appear in AI HTML: %s", got)
	}
	if !strings.Contains(got, "MAX") {
		t.Fatalf("non-index product name text should remain: %s", got)
	}
}

func TestAIJobQueueAcceptsOneThousandItems(t *testing.T) {
	queue := newAIJobQueue()
	for index := 0; index < 1000; index++ {
		queue.Push(aiCatalogJob{ItemID: string(rune(index + 1)), Generation: 1})
	}
	for index := 0; index < 1000; index++ {
		if _, ok := queue.Pop(); !ok {
			t.Fatalf("queue closed at item %d", index)
		}
	}
	queue.Close()
}

func TestGenerateReusesPersistentCacheWithoutStartingModel(t *testing.T) {
	cacheDir := t.TempDir()
	processor := NewDescriptionProcessor(t.TempDir(), cacheDir)
	item := CatalogItem{ItemID: "one", TowKod: "MAX-0949", NameB2B: "Rora niklovana dupla", OriginalTechnicalDescription: "Početni opis"}
	key := descriptionCacheKey(item, item.OriginalTechnicalDescription)
	processor.saveCache(key, descriptionCacheEntry{HTML: `<h3 onclick="x">🚗 Sačuvan opis</h3><script>bad()</script>`, Source: "catalog"})

	got, source, err := processor.Generate(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if source != "cache" || got != "<h3>🚗 Sačuvan opis</h3>" {
		t.Fatalf("expected sanitized cache hit, got source=%q html=%q", source, got)
	}
	if _, err := os.Stat(processor.modelPath); !os.IsNotExist(err) {
		t.Fatalf("test must prove the model was not required, stat err=%v", err)
	}
}

func TestCachedAIHTMLRemovesSupplierIndex(t *testing.T) {
	cacheDir := t.TempDir()
	processor := NewDescriptionProcessor(t.TempDir(), cacheDir)
	item := CatalogItem{ItemID: "one", TowKod: "MAX-0949", NameB2B: "Rora MAX 0949", Index: "0949", OriginalTechnicalDescription: "Početni opis"}
	processor.saveCache(descriptionCacheKey(item, item.OriginalTechnicalDescription), descriptionCacheEntry{HTML: "<h3>Rora MAX 0949</h3><p>Model 0949</p>", Source: "catalog"})

	got, _, err := processor.Generate(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "0949") || !strings.Contains(got, "MAX") {
		t.Fatalf("cached AI HTML should keep the name but remove the index: %s", got)
	}
}

func TestDecodeGeneratedDescriptionIgnoresTrailingModelText(t *testing.T) {
	content := "```json\n{\"title\":\"Rora\",\"intro\":[],\"features\":[],\"dimensions\":[],\"notes\":[]}\"\nDodatni tekst koji model nije trebao vratiti"
	got, err := decodeGeneratedDescription(content)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Rora" {
		t.Fatalf("expected extracted JSON title, got %#v", got)
	}
}
