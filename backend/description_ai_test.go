package main

import "testing"

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
