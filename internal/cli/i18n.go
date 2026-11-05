package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

// i18nCmd translates the user interface.
//
//	ewiki i18n status
//	ewiki i18n strings LANG [--filter todo|missing|outdated|custom|all] [--source ID]
//	ewiki i18n set LANG KEY TEXT                 ("" removes a custom text)
//	ewiki i18n export LANG [--format json|xliff|po] [--source ID] [--filter all|missing|custom] [> file]
//	ewiki i18n import LANG FILE|- [--format json|xliff|po]
//	ewiki i18n add LANG | remove LANG            configured languages
func (e *env) i18nCmd(args []string) error {
	usage := errors.New("usage: ewiki i18n status | strings LANG [--filter F] [--source ID] | set LANG KEY TEXT | export LANG [--format F] [--source ID] [--filter F] | import LANG FILE|- [--format F] | add LANG | remove LANG [--as USER]")
	flags, pos, err := flagArgs(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usage
	}
	as := asArgs(flags)
	switch {
	case pos[0] == "status" && len(pos) == 1:
		return e.api(append([]string{"GET", "/api/v1/i18n/status"}, as...))
	case pos[0] == "strings" && len(pos) == 2:
		return e.i18nStrings(pos[1], flags)
	case pos[0] == "set" && len(pos) == 4:
		body, _ := json.Marshal(map[string]string{pos[2]: pos[3]})
		return e.api(append([]string{"PUT", "/api/v1/admin/i18n/" + pos[1], string(body)}, as...))
	case pos[0] == "export" && len(pos) == 2:
		return e.api(append([]string{"GET", "/api/v1/admin/i18n/" + pos[1] + "/export" + query(flags, "format", "source", "filter")}, as...))
	case pos[0] == "import" && len(pos) == 3:
		var data []byte
		if pos[2] == "-" {
			data, err = io.ReadAll(e.stdin)
		} else {
			data, err = os.ReadFile(pos[2])
		}
		if err != nil {
			return err
		}
		return e.apiBody("POST", "/api/v1/admin/i18n/"+pos[1]+"/import"+query(flags, "format"), data, flags["as"])
	case pos[0] == "add" && len(pos) == 2:
		body, _ := json.Marshal(map[string]string{"code": pos[1]})
		return e.api(append([]string{"POST", "/api/v1/admin/languages", string(body)}, as...))
	case pos[0] == "remove" && len(pos) == 2:
		return e.api(append([]string{"DELETE", "/api/v1/admin/languages/" + pos[1]}, as...))
	}
	return usage
}

// i18nStrings prints the texts of a language as a table (key, status, text).
func (e *env) i18nStrings(lang string, flags map[string]string) error {
	return e.apiJSON("GET", "/api/v1/admin/i18n/"+lang, flags["as"], func(b []byte) error {
		var list []struct {
			Key, Source, Status string
			Plural              bool
			Forms               []string
			Reference, Value    map[string]string
		}
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		filter := flags["filter"]
		if filter == "" {
			filter = "todo"
		}
		for _, s := range list {
			if src := flags["source"]; src != "" && s.Source != src {
				continue
			}
			switch filter {
			case "todo":
				if s.Status != "missing" && s.Status != "outdated" {
					continue
				}
			case "missing", "outdated", "custom":
				if s.Status != filter {
					continue
				}
			}
			forms := []string{""}
			if s.Plural {
				forms = s.Forms
			}
			for _, f := range forms {
				k := s.Key
				if f != "" {
					k += "_" + f
				}
				e.printf("%s\t%s\t%s\t%q\t%q\n", k, s.Source, s.Status, s.Reference[f], s.Value[f])
			}
		}
		return nil
	})
}

// categoryCmd shows and translates the texts of categories.
//
//	ewiki category translations ID
//	ewiki category translate ID LANG [--title T] [--description D] [--confirm] [--as USER]
func (e *env) categoryCmd(args []string) error {
	usage := errors.New("usage: ewiki category translations ID | translate ID LANG [--title T] [--description D] [--confirm] [--as USER]")
	flags, pos, err := flagArgs(args, "confirm")
	if err != nil {
		return err
	}
	switch {
	case len(pos) == 2 && pos[0] == "translations":
		return e.apiJSON("GET", "/api/v1/categories/"+pos[1], flags["as"], func(b []byte) error {
			var r struct {
				Category struct {
					Titles            map[string]string `json:"titles"`
					OriginalLang      string            `json:"original_lang"`
					TextRev           int               `json:"text_rev"`
					TranslatedPercent int               `json:"translated_percent"`
					Translations      []struct {
						Lang, Status string
						BasedOn      int `json:"based_on"`
					} `json:"translations"`
				} `json:"category"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return err
			}
			c := r.Category
			e.printf("original %s (revision %d), %d %% translated\n", c.OriginalLang, c.TextRev, c.TranslatedPercent)
			for _, t := range c.Translations {
				e.printf("%s\t%s\t%d\t%q\n", t.Lang, t.Status, t.BasedOn, c.Titles[t.Lang])
			}
			return nil
		})
	case len(pos) == 3 && pos[0] == "translate":
		in := map[string]any{"confirm": flags["confirm"] == "true"}
		if v, ok := flags["title"]; ok {
			in["title"] = v
		}
		if v, ok := flags["description"]; ok {
			in["description"] = v
		}
		body, _ := json.Marshal(in)
		return e.api(append([]string{"PUT", "/api/v1/categories/" + pos[1] + "/translations/" + pos[2], string(body)}, asArgs(flags)...))
	}
	return usage
}
