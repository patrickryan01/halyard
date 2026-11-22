package ui

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var langs = []string{"es", "fr", "it", "de", "nb", "bg", "el", "ko", "ja", "vi", "hi", "zh-Hans", "ar"}

// English and thirteen languages: every phrase the window can show has a value in each language, and the values in
// braces (names, numbers) are the same in the translation as in the English.
func TestEveryLanguageHasEveryPhrase(t *testing.T) {
	var keys []string
	b, err := fs.ReadFile(assets, "web/i18n/keys.json")
	if err != nil || json.Unmarshal(b, &keys) != nil || len(keys) < 300 {
		t.Fatalf("the phrase list is missing or short: %v %d", err, len(keys))
	}
	hole := regexp.MustCompile(`\{\w+\}`)
	holes := func(s string) string { h := hole.FindAllString(s, -1); sort.Strings(h); return strings.Join(h, ",") }
	for _, l := range langs {
		raw, err := fs.ReadFile(assets, "web/i18n/"+l+".json")
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		var cat map[string]string
		if err := json.Unmarshal(raw, &cat); err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		missing, bad := 0, 0
		for _, k := range keys {
			v, ok := cat[k]
			if !ok || strings.TrimSpace(v) == "" {
				missing++
				continue
			}
			if holes(k) != holes(v) {
				bad++
				t.Errorf("%s: %q lost or changed a value in braces: %q", l, k, v)
			}
		}
		if missing > 0 {
			t.Errorf("%s: %d phrases have no translation", l, missing)
		}
		// the same phrase may stay the same word in a language, but a catalog that is mostly English is not a translation
		same := 0
		for k, v := range cat {
			if k == v {
				same++
			}
		}
		if same > len(keys)/8 {
			t.Errorf("%s: %d of %d phrases are still English", l, same, len(keys))
		}
	}
}

// Every language the program offers is a language with a catalog.
func TestLanguageListMatchesCatalogs(t *testing.T) {
	js, err := fs.ReadFile(assets, "web/panels.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range langs {
		if !strings.Contains(string(js), "['"+l+"', ") {
			t.Errorf("%s is not in the language chooser", l)
		}
	}
}
