package core_layout

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// ---------------------------------------------------------------- external video or embed

// embed shows a video or another page of a service outside the wiki. Nothing
// is loaded from that service until the reader agrees (two clicks): the
// server writes only a box with the name of the service and a button; the
// script puts the frame in after the click. Only services an administrator
// allowed (media.embed_allow) are accepted, and the same list opens the
// Content-Security-Policy (frame-src) for them.
type embed struct{ allow func() []string }

var embedRatios = []string{"16x9", "4x3", "1x1", "9x16"}

func (embed) Name() string { return "embed" }
func (embed) Labels() map[string]string {
	return lm("External video or embed", "Externes Video oder Einbettung")
}
func (embed) Description() map[string]string {
	return lm("A video or page of an allowed service; it loads only after the reader agrees.",
		"Ein Video oder eine Seite eines freigegebenen Dienstes; es lädt erst, wenn die lesende Person zustimmt.")
}
func (embed) HasBody() bool { return false }
func (embed) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "address", Labels: lm("Address (https)", "Adresse (https)"), Type: "url", Required: true},
		{Name: "title", Labels: lm("Title (what it shows)", "Titel (was es zeigt)"), Type: "text", Required: true},
		{Name: "ratio", Labels: lm("Aspect ratio", "Seitenverhältnis"), Type: "select", Options: embedRatios, Default: "16x9",
			OptionLabels: map[string]map[string]string{"16x9": lm("16:9 (wide)", "16:9 (breit)"), "4x3": lm("4:3", "4:3"), "1x1": lm("1:1 (square)", "1:1 (quadratisch)"), "9x16": lm("9:16 (upright, phone)", "9:16 (hoch, Handy)")}},
		{Name: "caption", Labels: lm("Caption", "Bildunterschrift"), Type: "text"},
	}
}

var (
	ytID    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	vimeoID = regexp.MustCompile(`^[0-9]{1,15}$`)
)

// embedTarget is what a link turns into: the address of the frame, its
// origin (for the allow list) and the address to open on the service itself.
type embedTarget struct {
	src, origin, host, link string
}

// EmbedTarget turns the address an author gave into the address of the
// frame. Links to watch pages of two common video services become their
// player addresses (the one of the first without cookies); every other
// address is used as it is. Only https, no user name or password, no
// fragment-only tricks; the result is rebuilt from its parsed parts.
func EmbedTarget(raw string) (embedTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return embedTarget{}, apperr.BadRequest("the embed needs an address")
	}
	if len(raw) > 2000 {
		return embedTarget{}, apperr.BadRequest("the address is too long (at most %d characters)", 2000)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return embedTarget{}, apperr.BadRequest("the address must start with https:// and name a server")
	}
	host := strings.ToLower(u.Hostname())
	start := ""
	if t := u.Query().Get("t"); t != "" {
		if n, err := strconv.Atoi(strings.TrimSuffix(t, "s")); err == nil && n > 0 && n < 100000 {
			start = strconv.Itoa(n)
		}
	}
	videoOf := func() string {
		switch host {
		case "youtube.com", "www.youtube.com", "m.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
			if id := u.Query().Get("v"); u.Path == "/watch" && ytID.MatchString(id) {
				return id
			}
			for _, p := range []string{"/embed/", "/shorts/", "/live/"} {
				if id, ok := strings.CutPrefix(u.Path, p); ok && ytID.MatchString(id) {
					return id
				}
			}
		case "youtu.be":
			if id := strings.TrimPrefix(u.Path, "/"); ytID.MatchString(id) {
				return id
			}
		}
		return ""
	}
	if id := videoOf(); id != "" {
		src := "https://www.youtube-nocookie.com/embed/" + id
		link := "https://www.youtube.com/watch?v=" + id
		if start != "" {
			src += "?start=" + start
			link += "&t=" + start + "s"
		}
		return embedTarget{src: src, origin: "https://www.youtube-nocookie.com", host: "www.youtube-nocookie.com", link: link}, nil
	}
	if host == "vimeo.com" || host == "www.vimeo.com" || host == "player.vimeo.com" {
		id := strings.TrimPrefix(strings.TrimPrefix(u.Path, "/video"), "/")
		if vimeoID.MatchString(id) {
			return embedTarget{src: "https://player.vimeo.com/video/" + id + "?dnt=1", origin: "https://player.vimeo.com", host: "player.vimeo.com",
				link: "https://vimeo.com/" + id}, nil
		}
	}
	u.Fragment, u.RawFragment = "", ""
	origin := "https://" + strings.ToLower(u.Host)
	return embedTarget{src: u.String(), origin: origin, host: host, link: u.String()}, nil
}

func (e embed) Render(_ context.Context, mc content.MacroContext) (string, error) {
	title, err := shortText("title", mc.Params["title"], 200)
	if err != nil {
		return "", err
	}
	if title == "" {
		return "", apperr.BadRequest("the embed needs a title that says what it shows")
	}
	caption, err := shortText("caption", mc.Params["caption"], 300)
	if err != nil {
		return "", err
	}
	tg, err := EmbedTarget(mc.Params["address"])
	if err != nil {
		return "", err
	}
	allowed := false
	if e.allow != nil {
		for _, o := range e.allow() {
			if strings.EqualFold(o, tg.origin) {
				allowed = true
			}
		}
	}
	if !allowed {
		return "", apperr.BadRequest("content from %s is not allowed in this wiki; an administrator can allow %s under Administration → Settings → Media", tg.host, tg.origin)
	}
	de := strings.HasPrefix(mc.Lang, "de")
	note, open, newWin := "This content comes from %s. Your browser connects to %s only when you load it; the service then receives data such as your IP address.", "Watch on %s", " (opens in a new window)"
	if de {
		note, open, newWin = "Dieser Inhalt kommt von %s. Ihr Browser verbindet sich erst mit %s, wenn Sie ihn laden; der Dienst erhält dann Daten wie Ihre IP-Adresse.", "Auf %s ansehen", " (öffnet ein neues Fenster)"
	}
	var b strings.Builder
	b.WriteString(`<figure class="ewl-embed ewl-embed--` + oneOf(mc.Params["ratio"], embedRatios) + `" data-ew-widget="ewl.embed" data-src="` + esc(tg.src) + `" data-host="` + esc(tg.host) + `" data-title="` + esc(title) + `">`)
	b.WriteString(`<div class="ewl-embed-box"><p class="ewl-embed-title">` + esc(title) + `</p>`)
	b.WriteString(`<p class="ewl-embed-note">` + esc(strings.ReplaceAll(note, "%s", tg.host)) + `</p>`)
	b.WriteString(`<p class="ewl-embed-link"><a href="` + esc(tg.link) + `" rel="noopener noreferrer" target="_blank">` + esc(strings.ReplaceAll(open, "%s", tg.host)) + `<span class="sr-only">` + esc(newWin) + `</span></a></p></div>`)
	if caption != "" {
		b.WriteString(`<figcaption>` + esc(caption) + `</figcaption>`)
	}
	b.WriteString(`</figure>`)
	return b.String(), nil
}
