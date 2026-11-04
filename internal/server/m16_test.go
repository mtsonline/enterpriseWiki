package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type uiString struct {
	Key       string            `json:"key"`
	Source    string            `json:"source"`
	Plural    bool              `json:"plural"`
	Forms     []string          `json:"forms"`
	Reference map[string]string `json:"reference"`
	Value     map[string]string `json:"value"`
	Status    string            `json:"status"`
}

func (c *client) postRaw(path string, body []byte) (int, []byte) {
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func find(list []uiString, key string) uiString {
	for _, s := range list {
		if s.Key == key {
			return s
		}
	}
	return uiString{}
}

// TestUITranslations covers plural texts, the permission per language,
// outdated translations and import/export (JSON, XLIFF, PO).
func TestUITranslations(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"

	// one permission per configured language (en, de, fr)
	var cat struct {
		Permissions []struct{ Key string } `json:"permissions"`
	}
	admin.must("GET", "/api/v1/permissions", nil, &cat, 200)
	keys := map[string]bool{}
	for _, p := range cat.Permissions {
		keys[p.Key] = true
	}
	for _, k := range []string{"core.i18n.translate.de", "core.i18n.translate.fr", "core.i18n.translate.en"} {
		if !keys[k] {
			t.Fatalf("permission %s missing", k)
		}
	}
	var role struct{ ID int64 }
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "French translators", "permissions": []string{"core.i18n.translate.fr"}}, &role, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "marie", "password": pw, "display_name": "Marie", "roles": []int64{role.ID}}, nil, 201)
	marie := e.client()
	marie.login("marie", pw)

	var fr []uiString
	marie.must("GET", "/api/v1/admin/i18n/fr", nil, &fr, 200)
	if code := marie.do("GET", "/api/v1/admin/i18n/de", nil, nil); code != 403 {
		t.Fatalf("German without permission: %d", code)
	}
	if code := marie.do("PUT", "/api/v1/admin/i18n/de", map[string]string{"common.save": "x"}, nil); code != 403 {
		t.Fatalf("German saved without permission: %d", code)
	}
	if code := marie.do("POST", "/api/v1/admin/languages", map[string]string{"code": "it"}, nil); code != 403 {
		t.Fatalf("translator added a language: %d", code)
	}
	var st []struct {
		Lang         string `json:"lang"`
		CanTranslate bool   `json:"can_translate"`
		Missing      int    `json:"missing"`
		Outdated     int    `json:"outdated"`
		Percent      int    `json:"percent"`
	}
	marie.must("GET", "/api/v1/i18n/status", nil, &st, 200)
	for _, s := range st {
		if s.CanTranslate != (s.Lang == "fr") {
			t.Fatalf("can_translate %s: %v", s.Lang, s.CanTranslate)
		}
	}

	// plural texts: forms of the language, reference per form
	n := find(fr, "inbox.new")
	if !n.Plural || strings.Join(n.Forms, ",") != "one,many,other" && strings.Join(n.Forms, ",") != "one,other" {
		t.Fatalf("plural text: %+v", n)
	}
	if n.Reference["one"] != "{n} new notification" || n.Status != "missing" {
		t.Fatalf("plural reference: %+v", n)
	}
	var res struct {
		Written int      `json:"written"`
		Unknown []string `json:"unknown"`
	}
	marie.must("PUT", "/api/v1/admin/i18n/fr", map[string]string{"inbox.new_one": "{n} nouvelle notification", "inbox.new_other": "{n} nouvelles notifications", "common.save": "Enregistrer"}, &res, 200)
	if res.Written != 3 {
		t.Fatalf("written: %+v", res)
	}
	if code := marie.do("PUT", "/api/v1/admin/i18n/fr", map[string]string{"no.such.key": "x"}, nil); code != 400 {
		t.Fatalf("unknown key accepted: %d", code)
	}
	var msgs map[string]string
	admin.must("GET", "/api/v1/i18n/fr", nil, &msgs, 200)
	if msgs["inbox.new_one"] != "{n} nouvelle notification" || msgs["common.save"] != "Enregistrer" {
		t.Fatalf("messages: %q %q", msgs["inbox.new_one"], msgs["common.save"])
	}

	// the English text changes: the French translation is outdated until confirmed
	admin.must("PUT", "/api/v1/admin/i18n/en", map[string]string{"common.save": "Save changes"}, nil, 200)
	marie.must("GET", "/api/v1/admin/i18n/fr", nil, &fr, 200)
	if s := find(fr, "common.save"); s.Status != "outdated" || s.Reference[""] != "Save changes" {
		t.Fatalf("not outdated: %+v", s)
	}
	marie.must("PUT", "/api/v1/admin/i18n/fr", map[string]string{"common.save": "Enregistrer"}, nil, 200) // still correct
	marie.must("GET", "/api/v1/admin/i18n/fr", nil, &fr, 200)
	if s := find(fr, "common.save"); s.Status != "custom" {
		t.Fatalf("confirmed: %+v", s)
	}

	// export and import: JSON, XLIFF and PO
	for _, f := range []string{"json", "xliff", "po"} {
		resp, body := marie.raw("/api/v1/admin/i18n/fr/export?format="+f+"&source=core&filter=custom", nil)
		if resp.StatusCode != 200 || !strings.Contains(body, "Enregistrer") {
			t.Fatalf("export %s: %d %s", f, resp.StatusCode, body)
		}
		if !strings.Contains(resp.Header.Get("Content-Disposition"), "ewiki-fr-core.") {
			t.Fatalf("export %s: %s", f, resp.Header.Get("Content-Disposition"))
		}
	}
	po := `msgid ""
msgstr "Language: fr\n"

#: core
msgctxt "common.cancel"
msgid "Cancel"
msgstr "Annuler"

#, fuzzy
msgctxt "common.close"
msgid "Close"
msgstr "Fermer"

msgctxt "does.not.exist"
msgid "x"
msgstr "y"
`
	code, b := marie.postRaw("/api/v1/admin/i18n/fr/import", []byte(po))
	if code != 200 {
		t.Fatalf("import po: %d %s", code, b)
	}
	_ = json.Unmarshal(b, &res)
	if res.Written != 1 || len(res.Unknown) != 1 {
		t.Fatalf("import result: %s", b)
	}
	code, b = marie.postRaw("/api/v1/admin/i18n/fr/import", []byte(`{"common.close": "Fermer"}`))
	if code != 200 || !strings.Contains(string(b), `"written": 1`) && !strings.Contains(string(b), `"written":1`) {
		t.Fatalf("import json: %d %s", code, b)
	}
	if code, _ := marie.postRaw("/api/v1/admin/i18n/de/import", []byte(`{"common.close": "x"}`)); code != 403 {
		t.Fatalf("import into German: %d", code)
	}
	xl := `<?xml version="1.0"?><xliff xmlns="urn:oasis:names:tc:xliff:document:2.0" version="2.0" srcLang="en" trgLang="de"><file id="core"/></xliff>`
	if code, _ := admin.postRaw("/api/v1/admin/i18n/fr/import?format=xliff", []byte(xl)); code != 400 {
		t.Fatalf("xliff of another language: %d", code)
	}
	admin.must("GET", "/api/v1/i18n/fr", nil, &msgs, 200)
	if msgs["common.cancel"] != "Annuler" || msgs["common.close"] != "Fermer" {
		t.Fatalf("imported: %q %q", msgs["common.cancel"], msgs["common.close"])
	}
	// removing a custom text restores the shipped one
	admin.must("PUT", "/api/v1/admin/i18n/en", map[string]string{"common.save": ""}, nil, 200)
	admin.must("GET", "/api/v1/i18n/en", nil, &msgs, 200)
	if msgs["common.save"] != "Save" {
		t.Fatalf("English restored: %q", msgs["common.save"])
	}
}

// TestCategoryTranslations covers the original language, outdated and
// confirmed translations and the progress of category texts.
func TestCategoryTranslations(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type trans struct {
		Lang    string `json:"lang"`
		Status  string `json:"status"`
		BasedOn int    `json:"based_on"`
	}
	type catT struct {
		ID           int64             `json:"id"`
		Titles       map[string]string `json:"titles"`
		OriginalLang string            `json:"original_lang"`
		TextRev      int               `json:"text_rev"`
		Translations []trans           `json:"translations"`
		Percent      int               `json:"translated_percent"`
		UpToDate     bool              `json:"translations_up_to_date"`
	}
	status := func(c catT) string {
		var parts []string
		for _, t := range c.Translations {
			parts = append(parts, t.Lang+"="+t.Status)
		}
		return strings.Join(parts, " ")
	}
	var c catT
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"de": "Personal", "en": "Staff"}, "original_lang": "de"}, &c, 201)
	if c.OriginalLang != "de" || status(c) != "de=original en=current fr=missing" || c.Percent != 50 {
		t.Fatalf("new: %s %d", status(c), c.Percent)
	}
	// the original changes: English is outdated
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID), map[string]any{"titles": map[string]string{"de": "Personalwesen", "en": "Staff"}}, &c, 200)
	if status(c) != "de=original en=outdated fr=missing" || c.Percent != 0 || c.TextRev != 2 {
		t.Fatalf("after change: %s %d rev %d", status(c), c.Percent, c.TextRev)
	}
	// still correct
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/en", map[string]any{"confirm": true}, &c, 200)
	if status(c) != "de=original en=current fr=missing" {
		t.Fatalf("confirmed: %s", status(c))
	}
	// translate into French
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/fr", map[string]any{"title": "Personnel", "description": "Ressources humaines"}, &c, 200)
	if status(c) != "de=original en=current fr=current" || !c.UpToDate || c.Percent != 100 {
		t.Fatalf("translated: %s", status(c))
	}
	// remove a translation
	c = catT{ID: c.ID} // decoding into an existing map would keep the old keys
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/fr", map[string]any{"title": ""}, &c, 200)
	if status(c) != "de=original en=current fr=missing" || c.Titles["fr"] != "" {
		t.Fatalf("removed: %s %v", status(c), c.Titles)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/de", map[string]any{"title": ""}, nil); code != 400 {
		t.Fatalf("original without title: %d", code)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/xx", map[string]any{"title": "x"}, nil); code != 400 {
		t.Fatalf("unknown language: %d", code)
	}
	// new original language: the others are based on it
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID), map[string]any{"original_lang": "en"}, &c, 200)
	if c.OriginalLang != "en" || status(c) != "en=original de=current fr=missing" {
		t.Fatalf("new original: %s", status(c))
	}
	// readers without manage permission cannot translate
	pw := "Passw0rd!Passw0rd"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": pw, "display_name": "Rita", "roles": []int64{}}, nil, 201)
	rita := e.client()
	rita.login("rita", pw)
	if code := rita.do("PUT", fmt.Sprintf("/api/v1/categories/%d", c.ID)+"/translations/fr", map[string]any{"title": "x"}, nil); code != 404 && code != 403 {
		t.Fatalf("translation without permission: %d", code)
	}
}
