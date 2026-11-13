// Workflow 2, part 3b: generate a file from a template (PDF, Word, Excel,
// CSV, JSON, text, Markdown) into the media library, and create an entry
// in a calendar. Both act through the wiki with the rights of the definer
// (or starter) and store id and address in fields of the case.
package workflow_process

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/ooxml"
	"github.com/mtsonline/enterprisewiki/internal/pdfdoc"
)

// FileFormats are the formats a file step can write.
var FileFormats = []string{"pdf", "docx", "xlsx", "csv", "json", "txt", "md"}

// FileSpec writes a document into a category of the media library.
type FileSpec struct {
	Category int64    `json:"category"`
	Name     string   `json:"name"`    // file name without extension (placeholders)
	Formats  []string `json:"formats"` // one or more of FileFormats
	// Content: the text with placeholders; lines starting with "# ", "## "
	// or "### " are headings (PDF, Word, text, Markdown).
	Content string `json:"content,omitempty"`
	// Data appends all fields of the case (label and value); tables (Excel,
	// CSV) always contain them.
	Data     bool   `json:"data,omitempty"`
	StoreID  string `json:"store_id,omitempty"`  // field: id of the (first) file
	StoreURL string `json:"store_url,omitempty"` // field: address of the (first) file
}

// CalendarSpec creates an entry in a calendar (plugin content.events).
type CalendarSpec struct {
	Calendar    int64  `json:"calendar"` // the calendar page
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	// Start: a date ("2026-12-01"), date and time ("2026-12-01 14:00"),
	// usually a placeholder ({{field.date}}), or relative: "+3d", "+2h".
	Start    string `json:"start"`
	Minutes  int    `json:"minutes,omitempty"` // duration (default 60; all day: whole days, at least one)
	AllDay   bool   `json:"all_day,omitempty"`
	TZ       string `json:"tz,omitempty"` // IANA time zone (default: the server's)
	Register bool   `json:"registration,omitempty"`
	StoreID  string `json:"store_id,omitempty"`
	StoreURL string `json:"store_url,omitempty"`
}

const maxFileContent = 200000

func (s *Step) validatePart3b(fs *FileSpec, cs *CalendarSpec) error {
	var err error
	s.NextElse = ""
	stores := func(ids ...string) error {
		for _, id := range ids {
			if id != "" {
				if !idRe.MatchString(id) {
					return apperr.BadRequest("step %q: field %q must consist of a-z, 0-9, _ and -", s.ID, id)
				}
			}
		}
		return nil
	}
	switch s.Type {
	case StepFile:
		if fs == nil {
			return apperr.BadRequest("step %q: the file is missing", s.ID)
		}
		f := *fs
		if f.Category <= 0 {
			return apperr.BadRequest("step %q: the category of the media library is required", s.ID)
		}
		if len(f.Formats) == 0 || len(f.Formats) > len(FileFormats) {
			return apperr.BadRequest("step %q: choose one or more formats (%s)", s.ID, strings.Join(FileFormats, ", "))
		}
		seen := map[string]bool{}
		for _, ff := range f.Formats {
			if !in(ff, FileFormats...) || seen[ff] {
				return apperr.BadRequest("step %q: unknown or repeated format %q", s.ID, ff)
			}
			seen[ff] = true
		}
		if f.Name, err = templ(f.Name, 200, "file name", s.ID); err != nil {
			return err
		}
		if f.Content, err = templ(f.Content, maxFileContent, "content", s.ID); err != nil {
			return err
		}
		if strings.TrimSpace(f.Content) == "" && !f.Data {
			return apperr.BadRequest("step %q: write a content or include the fields of the case", s.ID)
		}
		if err := stores(f.StoreID, f.StoreURL); err != nil {
			return err
		}
		s.File = &f
	case StepCalendar:
		if cs == nil {
			return apperr.BadRequest("step %q: the calendar entry is missing", s.ID)
		}
		c := *cs
		if c.Calendar <= 0 {
			return apperr.BadRequest("step %q: the calendar is required", s.ID)
		}
		if c.Title, err = templ(c.Title, 200, "title", s.ID); err != nil || strings.TrimSpace(c.Title) == "" {
			return apperr.BadRequest("step %q: the title is required", s.ID)
		}
		if c.Description, err = templ(c.Description, 50000, "description", s.ID); err != nil {
			return err
		}
		if c.Location, err = templ(c.Location, 300, "location", s.ID); err != nil {
			return err
		}
		if c.Start, err = templ(c.Start, 100, "start", s.ID); err != nil || strings.TrimSpace(c.Start) == "" {
			return apperr.BadRequest("step %q: the start is required (date, placeholder or +3d)", s.ID)
		}
		if c.Minutes < 0 || c.Minutes > 366*24*60 {
			return apperr.BadRequest("step %q: the duration must be between 0 and 366 days", s.ID)
		}
		if c.TZ != "" {
			if _, err := time.LoadLocation(c.TZ); err != nil {
				return apperr.BadRequest("step %q: unknown time zone %q", s.ID, c.TZ)
			}
		}
		if err := stores(c.StoreID, c.StoreURL); err != nil {
			return err
		}
		s.Calendar = &c
	}
	return nil
}

func fileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	if s == "" || s == "." || s == ".." {
		s = "document"
	}
	return s
}

// cellSafe keeps values from becoming formulas in spreadsheets (CSV).
func cellSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// record lists the fields of the case: label and value.
func record(x *Instance, vars map[string]string) [][2]string {
	var out [][2]string
	for _, f := range x.Definition.Fields {
		if f.Type == "section" {
			continue
		}
		out = append(out, [2]string{f.Label, vars["field."+f.ID]})
	}
	return out
}

func (pl *Plugin) runFile(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	f := s.File
	vars := pl.vars(ctx, tx, x, actorInfo{})
	title := strings.TrimSpace(render(f.Name, vars))
	if title == "" {
		title = fmt.Sprintf("%s %d", x.Title, x.ID)
	}
	base := fileName(title)
	content := render(f.Content, vars)
	rec := [][2]string(nil)
	if f.Data {
		rec = record(x, vars)
	}
	var first *media.Medium
	for _, format := range f.Formats {
		var data []byte
		var err error
		switch format {
		case "pdf":
			var lines []pdfdoc.Line
			for _, l := range strings.Split(content, "\n") {
				switch {
				case strings.HasPrefix(l, "# "):
					lines = append(lines, pdfdoc.Line{Text: l[2:], Bold: true, Size: 16, Space: 6})
				case strings.HasPrefix(l, "## "):
					lines = append(lines, pdfdoc.Line{Text: l[3:], Bold: true, Size: 13, Space: 6})
				case strings.HasPrefix(l, "### "):
					lines = append(lines, pdfdoc.Line{Text: l[4:], Bold: true, Space: 4})
				default:
					lines = append(lines, pdfdoc.Line{Text: l})
				}
			}
			for _, r := range rec {
				lines = append(lines, pdfdoc.Line{Text: r[0], Bold: true, Space: 8}, pdfdoc.Line{Text: r[1]})
			}
			data = pdfdoc.Write(title, x.Lang, lines)
		case "docx":
			var paras []ooxml.Paragraph
			var text []string
			flush := func() {
				if len(text) > 0 {
					paras = append(paras, ooxml.Paragraph{Text: strings.Join(text, "\n")})
					text = nil
				}
			}
			for _, l := range strings.Split(content, "\n") {
				switch {
				case strings.HasPrefix(l, "# "):
					flush()
					paras = append(paras, ooxml.Paragraph{Text: l[2:], Heading: 1})
				case strings.HasPrefix(l, "## "):
					flush()
					paras = append(paras, ooxml.Paragraph{Text: l[3:], Heading: 2})
				case strings.HasPrefix(l, "### "):
					flush()
					paras = append(paras, ooxml.Paragraph{Text: l[4:], Heading: 3})
				case strings.TrimSpace(l) == "":
					flush()
				default:
					text = append(text, l)
				}
			}
			flush()
			for _, r := range rec {
				paras = append(paras, ooxml.Paragraph{Text: r[0], Bold: true}, ooxml.Paragraph{Text: r[1]})
			}
			data, err = ooxml.Docx(title, x.Lang, paras)
		case "xlsx", "csv":
			rows := [][]string{{msg(x.Lang, "field"), msg(x.Lang, "value")}}
			for _, r := range record(x, vars) { // tables always hold the fields
				rows = append(rows, []string{r[0], r[1]})
			}
			if format == "xlsx" {
				data, err = ooxml.Xlsx(title, x.Lang, title, rows)
				break
			}
			var b bytes.Buffer
			w := csv.NewWriter(&b)
			for _, r := range rows {
				_ = w.Write([]string{cellSafe(r[0]), cellSafe(r[1])})
			}
			w.Flush()
			data = append([]byte("\xef\xbb\xbf"), b.Bytes()...)
		case "json":
			data, err = json.MarshalIndent(map[string]any{"workflow": vars["workflow.title"], "case": x.ID, "title": x.Title,
				"generated_at": time.Now().UTC(), "content": content, "data": x.Data}, "", "  ")
		case "txt", "md":
			var b strings.Builder
			b.WriteString(content)
			if len(rec) > 0 {
				if content != "" {
					b.WriteString("\n\n")
				}
				for _, r := range rec {
					if format == "md" {
						fmt.Fprintf(&b, "- **%s:** %s\n", r[0], r[1])
					} else {
						fmt.Fprintf(&b, "%s: %s\n", r[0], r[1])
					}
				}
			}
			data = []byte(b.String())
		}
		if err != nil {
			return result{}, err
		}
		// the media library checks the right to upload into the category
		m, err := pl.host.Media.Upload(ctx, media.UploadInput{CategoryID: f.Category, Name: base + "." + format, Title: title,
			Description: fmt.Sprintf("%s · #%d", vars["workflow.title"], x.ID)}, bytes.NewReader(data))
		if err != nil {
			return result{}, err
		}
		if first == nil {
			first = m
		}
	}
	if f.StoreID != "" {
		x.Data[f.StoreID] = strconv.FormatInt(first.ID, 10)
	}
	if f.StoreURL != "" {
		x.Data[f.StoreURL] = fmt.Sprintf("%s/media?category=%d&open=%d", vars["site.url"], f.Category, first.ID)
	}
	if err := saveData(ctx, tx, x); err != nil {
		return result{}, err
	}
	return result{next: s.Next, action: "file", label: base, note: fmt.Sprintf("%s · %d", strings.Join(f.Formats, ", "), first.ID)}, nil
}

var relTime = regexp.MustCompile(`^\+(\d{1,4})([dhm])$`)

// startTime reads the start of a calendar entry: a date, a date and time,
// RFC 3339 or a time relative to now (+3d, +2h, +30m).
func startTime(s string, loc *time.Location, now time.Time) (time.Time, bool, error) {
	s = strings.TrimSpace(s)
	if m := relTime.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		d := map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "m": time.Minute}[m[2]]
		return now.Add(time.Duration(n) * d), m[2] == "d", nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, false, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is no date (2026-12-01, 2026-12-01 14:00 or +3d)", s)
}

func defaultTZ() string {
	if n := time.Local.String(); n != "" && n != "Local" {
		return n
	}
	return "UTC"
}

func (pl *Plugin) runCalendar(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	c := s.Calendar
	vars := pl.vars(ctx, tx, x, actorInfo{})
	tz := c.TZ
	if tz == "" {
		tz = defaultTZ()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return result{}, err
	}
	start, dateOnly, err := startTime(render(c.Start, vars), loc, time.Now().In(loc))
	if err != nil {
		return result{}, err
	}
	allDay := c.AllDay || (dateOnly && c.Minutes == 0)
	minutes := c.Minutes
	if minutes == 0 {
		minutes = 60
		if allDay {
			minutes = 24 * 60
		}
	}
	title := strings.TrimSpace(render(c.Title, vars))
	in := map[string]any{"title": title, "description": render(c.Description, vars), "location": strings.TrimSpace(render(c.Location, vars)),
		"starts_at": start, "ends_at": start.Add(time.Duration(minutes) * time.Minute), "all_day": allDay, "tz": tz,
		"registration": c.Register, "url": vars["case.url"]}
	if !strings.HasPrefix(vars["case.url"], "http") {
		delete(in, "url")
	}
	// the calendar checks the right to create entries
	code, b, err := pl.call(ctx, http.MethodPost, fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", c.Calendar), in)
	if err != nil {
		return result{}, err
	}
	if code == http.StatusNotFound {
		return result{}, fmt.Errorf("calendar %d not found (or the calendar plugin is off)", c.Calendar)
	}
	if code != http.StatusCreated {
		return result{}, apiError(code, b)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &ev); err != nil {
		return result{}, err
	}
	if c.StoreID != "" {
		x.Data[c.StoreID] = strconv.FormatInt(ev.ID, 10)
	}
	if c.StoreURL != "" {
		x.Data[c.StoreURL] = fmt.Sprintf("%s/e/%d", vars["site.url"], c.Calendar)
	}
	if err := saveData(ctx, tx, x); err != nil {
		return result{}, err
	}
	return result{next: s.Next, action: "calendar", label: title, note: start.In(loc).Format("2006-01-02 15:04 MST")}, nil
}
