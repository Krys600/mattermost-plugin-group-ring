// Package i18n provides server-side translations. Catalogs are JSON files in translations/,
// named after the locale (for example "en.json"), mapping message IDs to text/template strings.
// English is the default and the fallback for missing locales or messages.
package i18n

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"text/template"
)

// DefaultLocale is used when a user's locale has no catalog or lacks a message.
const DefaultLocale = "en"

//go:embed translations/*.json
var catalogFiles embed.FS

// Bundle holds the parsed catalogs.
type Bundle struct {
	catalogs map[string]map[string]*template.Template
}

// Load parses every embedded catalog.
func Load() (*Bundle, error) {
	entries, err := catalogFiles.ReadDir("translations")
	if err != nil {
		return nil, err
	}
	b := &Bundle{catalogs: map[string]map[string]*template.Template{}}
	for _, entry := range entries {
		locale := strings.TrimSuffix(entry.Name(), ".json")
		data, err := catalogFiles.ReadFile(path.Join("translations", entry.Name()))
		if err != nil {
			return nil, err
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			return nil, fmt.Errorf("invalid catalog %s: %w", entry.Name(), err)
		}
		catalog := make(map[string]*template.Template, len(messages))
		for id, text := range messages {
			tmpl, err := template.New(id).Option("missingkey=zero").Parse(text)
			if err != nil {
				return nil, fmt.Errorf("invalid message %s in %s: %w", id, entry.Name(), err)
			}
			catalog[id] = tmpl
		}
		b.catalogs[strings.ToLower(locale)] = catalog
	}
	if _, ok := b.catalogs[DefaultLocale]; !ok {
		return nil, fmt.Errorf("missing default catalog %s.json", DefaultLocale)
	}
	return b, nil
}

func (b *Bundle) lookup(locale, id string) *template.Template {
	locale = strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	candidates := []string{locale}
	if lang, _, found := strings.Cut(locale, "-"); found {
		candidates = append(candidates, lang)
	}
	candidates = append(candidates, DefaultLocale)
	for _, candidate := range candidates {
		if tmpl, ok := b.catalogs[candidate][id]; ok {
			return tmpl
		}
	}
	return nil
}

// T returns the message for id in the given locale, with params substituted.
// It returns the id itself when the message does not exist.
func (b *Bundle) T(locale, id string, params map[string]any) string {
	tmpl := b.lookup(locale, id)
	if tmpl == nil {
		return id
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return id
	}
	return buf.String()
}
