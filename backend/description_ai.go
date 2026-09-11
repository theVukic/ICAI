package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	descriptionPromptVersion = "asi-description-v2-no-index"
	descriptionModelName     = "Qwen3-4B-Q4_K_M"
	descriptionModelSHA256   = "7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5"
	descriptionModelSize     = int64(2497280256)
)

type descriptionEvidence struct {
	ID   string `json:"id"`
	URL  string `json:"url,omitempty"`
	Text string `json:"text"`
}

type generatedDescriptionClaim struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
}

type generatedDescription struct {
	Title      string                      `json:"title"`
	Intro      []string                    `json:"intro"`
	Features   []generatedDescriptionClaim `json:"features"`
	Dimensions []generatedDescriptionClaim `json:"dimensions"`
	Notes      []generatedDescriptionClaim `json:"notes"`
}

type descriptionCacheEntry struct {
	HTML      string    `json:"html"`
	Source    string    `json:"source"`
	Sources   []string  `json:"sources,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type DescriptionProcessor struct {
	modelPath  string
	serverPath string
	cacheDir   string
	client     *http.Client

	serverMu  sync.Mutex
	serverCmd *exec.Cmd
	serverURL string
	serverErr error

	searchMu   sync.Mutex
	lastSearch time.Time
}

func NewDescriptionProcessor(modelDir, cacheDir string) *DescriptionProcessor {
	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName += ".exe"
	}
	return &DescriptionProcessor{
		modelPath:  filepath.Join(modelDir, "Qwen3-4B-Q4_K_M.gguf"),
		serverPath: filepath.Join(modelDir, serverName),
		cacheDir:   cacheDir,
		client: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 || !publicHostname(req.URL.Hostname()) {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

func (p *DescriptionProcessor) Status() (bool, string) {
	if info, err := os.Stat(p.modelPath); err != nil || info.IsDir() || info.Size() != descriptionModelSize {
		return false, "lokalni model za opise nije pronađen"
	}
	if info, err := os.Stat(p.serverPath); err != nil || info.IsDir() || info.Size() == 0 {
		return false, "llama.cpp runtime za opise nije pronađen"
	}
	return true, ""
}

func (p *DescriptionProcessor) Stop() {
	p.serverMu.Lock()
	defer p.serverMu.Unlock()
	if p.serverCmd != nil && p.serverCmd.Process != nil {
		_ = p.serverCmd.Process.Kill()
		_, _ = p.serverCmd.Process.Wait()
	}
	p.serverCmd = nil
	p.serverURL = ""
}

func (p *DescriptionProcessor) ResetFailure() {
	p.serverMu.Lock()
	defer p.serverMu.Unlock()
	p.serverErr = nil
}

func (p *DescriptionProcessor) Generate(ctx context.Context, item CatalogItem) (string, string, error) {
	original := strings.TrimSpace(item.OriginalTechnicalDescription)
	if original == "" {
		original = strings.TrimSpace(item.TechnicalDescription)
	}
	if original == "" {
		return "", "", errors.New("artikal nema početnu deskripciju")
	}

	cacheKey := descriptionCacheKey(item, original)
	if cached, ok := p.loadCache(cacheKey); ok {
		return sanitizeDescriptionHTML(removeCatalogIndex(cached.HTML, item)), "cache", nil
	}

	evidence := []descriptionEvidence{{ID: "catalog", Text: truncateText(original, 8000)}}
	webEvidence := p.searchProduct(ctx, item)
	evidence = append(evidence, webEvidence...)

	serverURL, err := p.ensureServer(ctx)
	if err != nil {
		return "", "", err
	}
	generated, err := p.requestDescription(ctx, serverURL, item, evidence)
	if err != nil {
		return "", "", err
	}
	validated := validateGeneratedDescription(generated, evidence, item)
	rendered := renderGeneratedDescription(validated, item, original)
	if strings.TrimSpace(rendered) == "" {
		return "", "", errors.New("lokalni AI nije vratio upotrebljivu deskripciju")
	}

	source := "catalog"
	sourceURLs := make([]string, 0, len(webEvidence))
	if len(webEvidence) > 0 {
		source = "web"
		for _, proof := range webEvidence {
			sourceURLs = append(sourceURLs, proof.URL)
		}
	}
	p.saveCache(cacheKey, descriptionCacheEntry{HTML: rendered, Source: source, Sources: sourceURLs, CreatedAt: time.Now()})
	logEvent("ai-description", "description generated", map[string]any{
		"item_id": item.ItemID, "source": source, "sources": sourceURLs, "model": descriptionModelName,
	})
	return rendered, source, nil
}

func descriptionCacheKey(item CatalogItem, original string) string {
	value := strings.Join([]string{
		descriptionPromptVersion,
		descriptionModelSHA256,
		strings.TrimSpace(item.NameB2B),
		strings.TrimSpace(item.BrandName),
		strings.TrimSpace(item.TowKod),
		strings.TrimSpace(item.Index),
		original,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (p *DescriptionProcessor) loadCache(key string) (descriptionCacheEntry, bool) {
	input, err := os.ReadFile(filepath.Join(p.cacheDir, key+".json"))
	if err != nil {
		return descriptionCacheEntry{}, false
	}
	var entry descriptionCacheEntry
	if json.Unmarshal(input, &entry) != nil || strings.TrimSpace(entry.HTML) == "" {
		return descriptionCacheEntry{}, false
	}
	return entry, true
}

func (p *DescriptionProcessor) saveCache(key string, entry descriptionCacheEntry) {
	if os.MkdirAll(p.cacheDir, 0o755) != nil {
		return
	}
	output, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}
	tempPath := filepath.Join(p.cacheDir, key+".tmp")
	finalPath := filepath.Join(p.cacheDir, key+".json")
	if os.WriteFile(tempPath, append(output, '\n'), 0o644) == nil {
		_ = os.Rename(tempPath, finalPath)
	}
}

func (p *DescriptionProcessor) ensureServer(ctx context.Context) (string, error) {
	p.serverMu.Lock()
	defer p.serverMu.Unlock()
	if p.serverURL != "" && p.serverHealthy(ctx, p.serverURL) {
		return p.serverURL, nil
	}
	if p.serverErr != nil {
		return "", p.serverErr
	}
	if ready, reason := p.Status(); !ready {
		return "", errors.New(reason)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("nije moguće rezervisati lokalni AI port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cmd := exec.Command(p.serverPath,
		"-m", p.modelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"-c", "8192",
		"-np", "1",
		"--jinja",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	if err := cmd.Start(); err != nil {
		p.serverErr = fmt.Errorf("nije moguće pokrenuti lokalni AI: %w", err)
		return "", p.serverErr
	}
	p.serverCmd = cmd
	p.serverURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if p.serverHealthy(ctx, p.serverURL) {
			return p.serverURL, nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	p.serverErr = errors.New("lokalni AI se nije učitao u predviđenom vremenu")
	return "", p.serverErr
}

func (p *DescriptionProcessor) serverHealthy(ctx context.Context, baseURL string) bool {
	healthCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false
	}
	response, err := p.client.Do(req)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}

func (p *DescriptionProcessor) requestDescription(ctx context.Context, baseURL string, item CatalogItem, evidence []descriptionEvidence) (generatedDescription, error) {
	evidenceJSON, _ := json.Marshal(evidence)
	prompt := fmt.Sprintf(`/no_think
Napiši prodajnu deskripciju proizvoda na srpskoj latinici, ijekavica. Engleske riječi koristi samo za brend ili termin bez prirodnog prevoda.
Stil treba ličiti na uredan oglas: emoji, kratak naslov, uvod, karakteristike, opcionalne dimenzije i napomena. U tekstu možeš koristiti **bold** i *italic*.
	Ne izmišljaj činjenice. Svaka karakteristika, dimenzija ili napomena mora navesti evidence ID koji je direktno podržava. Ako web dokaz ne potvrđuje tačan proizvod, koristi samo catalog. Ne prikazuj izvore kupcu. Ne navodi interni indeks proizvoda nigdje u tekstu, uključujući naslov.
Vrati isključivo JSON oblika:
{"title":"...","intro":["..."],"features":[{"text":"...","evidence":["catalog"]}],"dimensions":[],"notes":[]}

	Proizvod:
	- naziv: %s
	- brend: %s
	- šifra: %s

	Dokazi:
	%s`, item.NameB2B, item.BrandName, item.TowKod, string(evidenceJSON))

	payload := map[string]any{
		"model":           "local",
		"temperature":     0.25,
		"max_tokens":      1400,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return generatedDescription{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return generatedDescription{}, fmt.Errorf("lokalni AI zahtjev nije uspio: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return generatedDescription{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return generatedDescription{}, fmt.Errorf("lokalni AI je vratio status %d", response.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || len(result.Choices) == 0 {
		return generatedDescription{}, errors.New("lokalni AI odgovor nije validan")
	}
	content := strings.TrimSpace(result.Choices[0].Message.Content)
	generated, err := decodeGeneratedDescription(content)
	if err != nil {
		return generatedDescription{}, fmt.Errorf("lokalni AI nije vratio očekivani JSON: %w", err)
	}
	return generated, nil
}

// decodeGeneratedDescription accepts the JSON object even when a local model
// appends a stray quote, explanation, or closing Markdown fence after it.
// The extracted object is still decoded strictly, so malformed product data is
// never accepted.
func decodeGeneratedDescription(content string) (generatedDescription, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSpace(content)

	object, ok := firstJSONObject(content)
	if !ok {
		return generatedDescription{}, errors.New("AI odgovor ne sadrži kompletan JSON objekat")
	}
	var generated generatedDescription
	if err := json.Unmarshal([]byte(object), &generated); err != nil {
		// Small local models occasionally emit literal line breaks/tabs inside a
		// JSON string, or a trailing comma before ]/}. Repair only those JSON
		// formatting defects and still strictly decode the resulting object.
		repaired := repairModelJSON(object)
		if repaired == object {
			return generatedDescription{}, err
		}
		if repairedErr := json.Unmarshal([]byte(repaired), &generated); repairedErr != nil {
			return generatedDescription{}, repairedErr
		}
	}
	return generated, nil
}

func repairModelJSON(value string) string {
	var output strings.Builder
	output.Grow(len(value) + 16)
	inString := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if inString {
			if escaped {
				escaped = false
				output.WriteByte(character)
				continue
			}
			if character == '\\' {
				escaped = true
				output.WriteByte(character)
				continue
			}
			if character == '"' {
				inString = false
				output.WriteByte(character)
				continue
			}
			switch character {
			case '\n':
				output.WriteString(`\n`)
			case '\r':
				output.WriteString(`\r`)
			case '\t':
				output.WriteString(`\t`)
			default:
				output.WriteByte(character)
			}
			continue
		}
		if character == '"' {
			inString = true
		}
		output.WriteByte(character)
	}

	// Remove a comma followed only by whitespace and a closing JSON delimiter.
	// Models can produce every variant: `,]`, `, }`, or a newline between them.
	return regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(output.String(), "$1")
}

func firstJSONObject(value string) (string, bool) {
	start := strings.IndexByte(value, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(value); index++ {
		character := value[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return value[start : index+1], true
			}
		}
	}
	return "", false
}

func (p *DescriptionProcessor) searchProduct(ctx context.Context, item CatalogItem) []descriptionEvidence {
	searchCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ctx = searchCtx
	p.searchMu.Lock()
	wait := 1500*time.Millisecond - time.Since(p.lastSearch)
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			p.searchMu.Unlock()
			return nil
		case <-timer.C:
		}
	}
	p.lastSearch = time.Now()
	p.searchMu.Unlock()

	query := strings.TrimSpace(strings.Join([]string{item.BrandName, item.NameB2B, item.TowKod, item.Index}, " "))
	if query == "" {
		return nil
	}
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ASI/1.0")
	response, err := p.client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil
	}
	links := extractDuckDuckGoLinks(string(body))
	result := make([]descriptionEvidence, 0, 3)
	for _, link := range links {
		if len(result) >= 3 || ctx.Err() != nil {
			break
		}
		text, ok := p.fetchPublicPage(ctx, link)
		if !ok || !productEvidenceMatches(item, text) {
			continue
		}
		result = append(result, descriptionEvidence{ID: fmt.Sprintf("web%d", len(result)+1), URL: link, Text: text})
	}
	return result
}

var hrefPattern = regexp.MustCompile(`(?is)href=["']([^"']+)["']`)

func extractDuckDuckGoLinks(document string) []string {
	seen := map[string]struct{}{}
	links := make([]string, 0, 10)
	for _, match := range hrefPattern.FindAllStringSubmatch(document, -1) {
		candidate := unescapeHTMLText(match[1])
		if strings.HasPrefix(candidate, "//") {
			candidate = "https:" + candidate
		}
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(parsed.Hostname()), "duckduckgo.com") {
			if redirected := parsed.Query().Get("uddg"); redirected != "" {
				candidate = redirected
			}
		}
		parsed, err = url.Parse(candidate)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		links = append(links, candidate)
		if len(links) == 10 {
			break
		}
	}
	return links
}

func (p *DescriptionProcessor) fetchPublicPage(ctx context.Context, target string) (string, bool) {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !publicHostname(parsed.Hostname()) {
		return "", false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", false
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ASI/1.0")
	response, err := p.client.Do(request)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", false
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "text/plain") {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 768<<10))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(htmlToText(string(body), 3500)), true
}

func publicHostname(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	addresses, err := net.LookupIP(host)
	if err != nil || len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
			return false
		}
	}
	return true
}

var (
	scriptBlockPattern = regexp.MustCompile(`(?is)<(?:script|style|noscript)[^>]*>.*?</(?:script|style|noscript)\s*>`)
	tagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	spacePattern       = regexp.MustCompile(`\s+`)
)

func htmlToText(document string, limit int) string {
	document = scriptBlockPattern.ReplaceAllString(document, " ")
	document = tagPattern.ReplaceAllString(document, " ")
	document = unescapeHTMLText(document)
	document = spacePattern.ReplaceAllString(document, " ")
	document = strings.TrimSpace(document)
	return truncateText(document, limit)
}

func truncateText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	characters := []rune(value)
	if len(characters) > limit {
		characters = characters[:limit]
	}
	return string(characters)
}

func productEvidenceMatches(item CatalogItem, text string) bool {
	normalized := normalizeEvidenceText(text)
	codeMatched := false
	for _, rawCode := range []string{item.TowKod, item.Index} {
		code := strings.TrimSpace(normalizeEvidenceText(rawCode))
		if len(code) >= 3 && strings.Contains(normalized, code) {
			codeMatched = true
			break
		}
	}
	if !codeMatched {
		return false
	}
	tokens := meaningfulTokens(item.BrandName + " " + item.NameB2B)
	matches := 0
	for _, token := range tokens {
		if strings.Contains(normalized, token) {
			matches++
		}
	}
	return matches >= 2 || (len(tokens) == 1 && matches == 1)
}

func normalizeEvidenceText(value string) string {
	value = strings.ToLower(unescapeHTMLText(value))
	replacer := strings.NewReplacer("č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "d")
	value = replacer.Replace(value)
	return spacePattern.ReplaceAllString(value, " ")
}

func meaningfulTokens(value string) []string {
	parts := regexp.MustCompile(`[^a-zA-Z0-9čćšžđČĆŠŽĐ]+`).Split(normalizeEvidenceText(value), -1)
	seen := map[string]struct{}{}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	sort.Strings(result)
	return result
}

func validateGeneratedDescription(generated generatedDescription, evidence []descriptionEvidence, item CatalogItem) generatedDescription {
	sources := make(map[string]string, len(evidence))
	for _, source := range evidence {
		sources[source.ID] = normalizeEvidenceText(source.Text)
	}
	allEvidence := ""
	for _, source := range sources {
		allEvidence += " " + source
	}
	generated.Title = strings.TrimSpace(generated.Title)
	if generated.Title == "" || !claimSupported(generated.Title, allEvidence) {
		generated.Title = item.NameB2B
	}
	generated.Intro = validateIntroLines(generated.Intro, allEvidence, 2)
	generated.Features = validateClaims(generated.Features, sources, 12)
	generated.Dimensions = validateClaims(generated.Dimensions, sources, 8)
	generated.Notes = validateClaims(generated.Notes, sources, 4)
	return generated
}

func validateIntroLines(values []string, evidence string, limit int) []string {
	result := make([]string, 0, limit)
	for _, value := range cleanLines(values, limit) {
		if claimSupported(value, evidence) {
			result = append(result, value)
		}
	}
	return result
}

func cleanLines(values []string, limit int) []string {
	result := make([]string, 0, limit)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
		if len(result) == limit {
			break
		}
	}
	return result
}

var numberPattern = regexp.MustCompile(`\b\d+(?:[.,]\d+)?\b`)
var guardedFactTerms = []string{"inox", "celik", "alumin", "univerzal", "precnik", "dimenz", "vijak", "pakov", "varen", "hrom", "nikl", "kompatibil", "vozil"}

func validateClaims(claims []generatedDescriptionClaim, sources map[string]string, limit int) []generatedDescriptionClaim {
	result := make([]generatedDescriptionClaim, 0, limit)
	for _, claim := range claims {
		claim.Text = strings.TrimSpace(claim.Text)
		if claim.Text == "" || len(claim.Evidence) == 0 {
			continue
		}
		combined := ""
		validEvidence := make([]string, 0, len(claim.Evidence))
		for _, id := range claim.Evidence {
			if source, ok := sources[id]; ok {
				combined += " " + source
				validEvidence = append(validEvidence, id)
			}
		}
		if len(validEvidence) == 0 || !claimSupported(claim.Text, combined) {
			continue
		}
		claim.Evidence = validEvidence
		result = append(result, claim)
		if len(result) == limit {
			break
		}
	}
	return result
}

func claimSupported(claim, evidence string) bool {
	normalizedClaim := normalizeEvidenceText(claim)
	for _, number := range numberPattern.FindAllString(normalizedClaim, -1) {
		if !strings.Contains(evidence, number) {
			return false
		}
	}
	for _, term := range guardedFactTerms {
		if strings.Contains(normalizedClaim, term) && !strings.Contains(evidence, term) {
			return false
		}
	}
	return true
}

func renderGeneratedDescription(generated generatedDescription, item CatalogItem, original string) string {
	generated = removeIndexFromGeneratedDescription(generated, item)
	original = removeCatalogIndex(original, item)
	var output strings.Builder
	title := strings.TrimSpace(generated.Title)
	if title == "" {
		title = removeCatalogIndex(firstNonEmpty(item.NameB2B, item.TowKod, "Proizvod"), item)
	}
	output.WriteString("<h3>🚗✨ ")
	output.WriteString(renderInlineMarkdown(title))
	output.WriteString("</h3>")
	for _, paragraph := range generated.Intro {
		output.WriteString("<p>")
		output.WriteString(renderInlineMarkdown(paragraph))
		output.WriteString("</p>")
	}
	if len(generated.Features) == 0 {
		generated.Features = []generatedDescriptionClaim{{Text: original, Evidence: []string{"catalog"}}}
	}
	renderClaimSection(&output, "✅ Karakteristike:", generated.Features)
	renderClaimSection(&output, "📏 Dimenzije:", generated.Dimensions)
	renderClaimSection(&output, "⚠️ Napomena:", generated.Notes)
	return sanitizeDescriptionHTML(output.String())
}

// Supplier indexes are useful internally and in the catalog, but they should
// not be repeated in customer-facing AI copy (for example "MAX 0949" becomes
// "MAX"). Only numeric/code-like parts of Index are removed so real product
// measurements remain untouched.
func removeIndexFromGeneratedDescription(generated generatedDescription, item CatalogItem) generatedDescription {
	generated.Title = removeCatalogIndex(generated.Title, item)
	for index := range generated.Intro {
		generated.Intro[index] = removeCatalogIndex(generated.Intro[index], item)
	}
	for _, group := range []*[]generatedDescriptionClaim{&generated.Features, &generated.Dimensions, &generated.Notes} {
		for index := range *group {
			(*group)[index].Text = removeCatalogIndex((*group)[index].Text, item)
		}
	}
	return generated
}

func removeCatalogIndex(value string, item CatalogItem) string {
	value = strings.TrimSpace(value)
	index := strings.TrimSpace(item.Index)
	if value == "" || index == "" {
		return value
	}
	candidates := numberPattern.FindAllString(index, -1)
	if !regexp.MustCompile(`[[:alpha:]]`).MatchString(index) {
		candidates = append(candidates, index)
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if len(candidate) < 2 {
			continue
		}
		if _, alreadyHandled := seen[candidate]; alreadyHandled {
			continue
		}
		seen[candidate] = struct{}{}
		pattern := regexp.MustCompile(`(?i)(^|[^[:alnum:]])` + regexp.QuoteMeta(candidate) + `([^[:alnum:]]|$)`)
		value = pattern.ReplaceAllString(value, "$1$2")
	}
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func renderClaimSection(output *strings.Builder, heading string, claims []generatedDescriptionClaim) {
	if len(claims) == 0 {
		return
	}
	output.WriteString("<p><strong>")
	output.WriteString(escapeHTMLText(heading))
	output.WriteString("</strong></p><ul>")
	for _, claim := range claims {
		output.WriteString("<li>")
		output.WriteString(renderInlineMarkdown(claim.Text))
		output.WriteString("</li>")
	}
	output.WriteString("</ul>")
}

var boldMarkdownPattern = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var italicMarkdownPattern = regexp.MustCompile(`\*([^*]+)\*`)

func renderInlineMarkdown(value string) string {
	escaped := escapeHTMLText(strings.TrimSpace(value))
	escaped = boldMarkdownPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
	escaped = italicMarkdownPattern.ReplaceAllString(escaped, "<em>$1</em>")
	return escaped
}

var dangerousHTMLBlockPattern = regexp.MustCompile(`(?is)<(?:script|style|iframe|object|embed|svg|math)[^>]*>.*?</(?:script|style|iframe|object|embed|svg|math)\s*>`)
var htmlTokenPattern = regexp.MustCompile(`(?is)<\s*(/?)\s*([a-z0-9]+)(?:\s[^>]*)?>`)

func sanitizeDescriptionHTML(value string) string {
	value = dangerousHTMLBlockPattern.ReplaceAllString(value, "")
	allowed := map[string]bool{"h3": true, "p": true, "strong": true, "em": true, "ul": true, "ol": true, "li": true, "br": true}
	var output strings.Builder
	position := 0
	for _, indexes := range htmlTokenPattern.FindAllStringSubmatchIndex(value, -1) {
		if indexes[0] > position {
			output.WriteString(escapeHTMLText(unescapeHTMLText(value[position:indexes[0]])))
		}
		closing := indexes[2] >= 0 && value[indexes[2]:indexes[3]] == "/"
		tag := strings.ToLower(value[indexes[4]:indexes[5]])
		if allowed[tag] {
			if closing && tag != "br" {
				output.WriteString("</" + tag + ">")
			} else if !closing {
				output.WriteString("<" + tag + ">")
			}
		}
		position = indexes[1]
	}
	if position < len(value) {
		output.WriteString(escapeHTMLText(unescapeHTMLText(value[position:])))
	}
	return strings.TrimSpace(output.String())
}

func escapeHTMLText(value string) string {
	return escapeTemplateHTML(value)
}

func unescapeHTMLText(value string) string {
	replacer := strings.NewReplacer(
		"&nbsp;", " ", "&#160;", " ",
		"&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&amp;", "&",
	)
	return replacer.Replace(value)
}
