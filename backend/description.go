package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleDescription(w http.ResponseWriter, r *http.Request) {
	if a.olxCategories == nil || !a.olxCategories.HasToken() {
		writeError(w, http.StatusBadRequest, "OLX token nije podešen. Dodaj token i ime profila u backend/token.json.")
		return
	}

	switch r.Method {
	case http.MethodGet:
		document, err := a.loadDescriptionDocument()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, document)
	case http.MethodPut:
		var req DescriptionSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "neispravan JSON body")
			return
		}

		document, err := a.saveDescriptionDocument(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		logEvent("description", "description document saved", map[string]any{
			"content_length": len(document.Content),
			"profile_name":   document.ProfileName,
		})
		writeJSON(w, http.StatusOK, document)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleDescriptionRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	towKod, err := itemCodeFromPath(strings.TrimPrefix(r.URL.Path, "/api/description/render"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	document, err := a.loadDescriptionDocument()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	item, getErr := a.store.GetItemByTowKod(towKod)
	if getErr != nil {
		writeError(w, http.StatusBadRequest, getErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, DescriptionRenderResponse{
		ProfileName: document.ProfileName,
		TowKod:      towKod,
		Content:     renderDescriptionTemplate(document.Content, item),
	})
	logEvent("description", "description rendered", map[string]any{
		"profile_name": document.ProfileName,
		"tow_kod":      towKod,
	})
}

func (a *App) loadDescriptionDocument() (DescriptionDocument, error) {
	profileName := a.activeDescriptionProfileName()
	path := a.descriptionDocumentPath(profileName)

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logEvent("description", "description file missing, using empty document", map[string]any{
				"path":         path,
				"profile_name": profileName,
			})
			return DescriptionDocument{
				ProfileName: profileName,
				Content:     "",
			}, nil
		}

		return DescriptionDocument{}, fmt.Errorf("nije moguće pročitati description fajl: %w", err)
	}

	var document DescriptionDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return DescriptionDocument{}, fmt.Errorf("description fajl nije validan JSON: %w", err)
	}

	document.ProfileName = profileName
	logEvent("description", "description document loaded", map[string]any{
		"path":         path,
		"profile_name": profileName,
	})
	return document, nil
}

func (a *App) saveDescriptionDocument(req DescriptionSaveRequest) (DescriptionDocument, error) {
	profileName := a.activeDescriptionProfileName()
	now := time.Now()
	document := DescriptionDocument{
		ProfileName: profileName,
		Content:     strings.TrimSpace(req.Content),
		UpdatedAt:   &now,
	}

	if err := os.MkdirAll(a.descriptionDir, 0o755); err != nil {
		return DescriptionDocument{}, fmt.Errorf("nije moguće kreirati description folder: %w", err)
	}

	output, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return DescriptionDocument{}, fmt.Errorf("nije moguće pripremiti description JSON: %w", err)
	}

	output = append(output, '\n')
	if err := os.WriteFile(a.descriptionDocumentPath(profileName), output, 0o644); err != nil {
		return DescriptionDocument{}, fmt.Errorf("nije moguće sačuvati description JSON: %w", err)
	}

	logEvent("file", "description file written", map[string]any{
		"path":         a.descriptionDocumentPath(profileName),
		"profile_name": profileName,
		"size_bytes":   len(output),
	})
	return document, nil
}

func (a *App) activeDescriptionProfileName() string {
	if a.olxCategories == nil {
		return "default"
	}

	profileName := strings.TrimSpace(a.olxCategories.ActiveProfileName())
	if profileName == "" {
		return "default"
	}

	return profileName
}

func (a *App) descriptionDocumentPath(profileName string) string {
	return filepath.Join(a.descriptionDir, sanitizeDescriptionFileName(profileName)+".json")
}

func sanitizeDescriptionFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}

	var builder strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '-' || char == '_':
			builder.WriteRune(char)
		case char == ' ':
			builder.WriteRune('_')
		}
	}

	safe := strings.Trim(builder.String(), "_")
	if safe == "" {
		return "default"
	}

	return safe
}

func renderDescriptionTemplate(template string, item CatalogItem) string {
	if strings.TrimSpace(template) == "" {
		return ""
	}

	replacements := templateValuesForItem(item)
	rendered := template
	for key, value := range replacements {
		rendered = strings.ReplaceAll(rendered, "${"+key+"}", value)
	}

	return rendered
}

func templateValuesForItem(item CatalogItem) map[string]string {
	priceText := "Na upit"
	if item.RetailPrice > 0 {
		priceText = formatTemplateNumber(item.RetailPrice) + " " + escapeTemplateHTML(formatTemplateCurrency(item.CurrencyCode))
	}

	return map[string]string{
		"towKod":               escapeTemplateHTML(item.TowKod),
		"index":                escapeTemplateHTML(item.Index),
		"nameB2B":              escapeTemplateHTML(item.NameB2B),
		"brandName":            escapeTemplateHTML(item.BrandName),
		"technicalDescription": plainTextToHTML(item.TechnicalDescription),
		"retailPrice":          priceText,
		"currencyCode":         escapeTemplateHTML(formatTemplateCurrency(item.CurrencyCode)),
		"sourceFileName":       escapeTemplateHTML(item.SourceFileName),
		"statusCode":           escapeTemplateHTML(item.Status.Code),
		"statusName":           escapeTemplateHTML(item.Status.Name),
	}
}

func plainTextToHTML(value string) string {
	escaped := escapeTemplateHTML(value)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\n")
	return strings.ReplaceAll(escaped, "\n", "<br>")
}

func escapeTemplateHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

func formatTemplateCurrency(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "BAM") {
		return "KM"
	}
	if strings.TrimSpace(value) == "" {
		return "EUR"
	}
	return strings.TrimSpace(value)
}

func formatTemplateNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}
