package core_layout

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// ---------------------------------------------------------------- map

// mapBlock shows a place on a map of an allowed map service, the same way
// as an embed: nothing loads from the service before the reader agrees
// (two clicks, media.embed_allow). The place is always written out as
// text, for everyone who cannot or does not want to load the map. With
// coordinates it uses OpenStreetMap; with an address it shows the map page
// of any other allowed service (for example a map server of one's own).
// A map block with markers of its own and keyboard use is planned for
// later (owner's decision of 24.11.2026).
type mapBlock struct{ allow func() []string }

const osmOrigin = "https://www.openstreetmap.org"

func (mapBlock) Name() string              { return "map" }
func (mapBlock) Group() string             { return "media" }
func (mapBlock) Labels() map[string]string { return lm("Map", "Karte") }
func (mapBlock) Description() map[string]string {
	return lm("A place on a map of an allowed service (OpenStreetMap from coordinates, or the address of another map). It loads only after the reader agrees; the place always stands as text below it.",
		"Ein Ort auf einer Karte eines freigegebenen Dienstes (OpenStreetMap aus Koordinaten oder die Adresse einer anderen Karte). Sie lädt erst, wenn die lesende Person zustimmt; der Ort steht immer als Text darunter.")
}
func (mapBlock) HasBody() bool { return false }
func (mapBlock) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "place", Labels: lm("The place in words (address, building, how to find it)", "Der Ort in Worten (Adresse, Gebäude, wie man hinfindet)"), Type: "text", Required: true},
		{Name: "lat", Labels: lm("Latitude (e.g. 46.6167)", "Breitengrad (z. B. 46.6167)"), Type: "text"},
		{Name: "lon", Labels: lm("Longitude (e.g. 14.2653)", "Längengrad (z. B. 14.2653)"), Type: "text"},
		{Name: "zoom", Labels: lm("Zoom (1 world … 19 house)", "Zoom (1 Welt … 19 Haus)"), Type: "number", Default: "16"},
		{Name: "address", Labels: lm("Or: address of another map (https, an allowed service)", "Oder: Adresse einer anderen Karte (https, freigegebener Dienst)"), Type: "url"},
		{Name: "ratio", Labels: lm("Aspect ratio", "Seitenverhältnis"), Type: "select", Options: embedRatios, Default: "16x9",
			OptionLabels: map[string]map[string]string{"16x9": lm("16:9 (wide)", "16:9 (breit)"), "4x3": lm("4:3", "4:3"), "1x1": lm("1:1 (square)", "1:1 (quadratisch)"), "9x16": lm("9:16 (upright, phone)", "9:16 (hoch, Handy)")}},
	}
}

var coordRe = regexp.MustCompile(`^-?[0-9]{1,3}(\.[0-9]{1,7})?$`)

// coord reads a coordinate: a decimal number with a dot, within ±limit.
func coord(name, s string, limit float64) (float64, error) {
	s = strings.TrimSpace(s)
	if !coordRe.MatchString(s) {
		return 0, apperr.BadRequest("%s: a decimal number with a dot, e.g. 46.6167", name)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.Abs(v) > limit {
		return 0, apperr.BadRequest("%s: from -%v to %v", name, limit, limit)
	}
	return v, nil
}

// osmTarget builds the embed address of OpenStreetMap for a point: a box
// around it that fits the zoom level, with a marker.
func osmTarget(lat, lon float64, zoom int) embedTarget {
	// a frame about 640 pixels wide: 2.5 tiles of 256 pixels
	dLon := 2.5 * 360 / math.Pow(2, float64(zoom)) / 2
	dLat := dLon * math.Cos(lat*math.Pi/180) * 0.6
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }
	box := fmt.Sprintf("%s,%s,%s,%s", f(math.Max(lon-dLon, -180)), f(math.Max(lat-dLat, -85)), f(math.Min(lon+dLon, 180)), f(math.Min(lat+dLat, 85)))
	src := osmOrigin + "/export/embed.html?bbox=" + box + "&layer=mapnik&marker=" + f(lat) + "%2C" + f(lon)
	link := fmt.Sprintf("%s/?mlat=%s&mlon=%s#map=%d/%s/%s", osmOrigin, f(lat), f(lon), zoom, f(lat), f(lon))
	return embedTarget{src: src, origin: osmOrigin, host: "www.openstreetmap.org", link: link}
}

func (m mapBlock) Render(_ context.Context, mc content.MacroContext) (string, error) {
	place, err := shortText("place", mc.Params["place"], 300)
	if err != nil {
		return "", err
	}
	if place == "" {
		return "", apperr.BadRequest("a map needs the place in words (place), for everyone who does not see the map")
	}
	var tg embedTarget
	address := strings.TrimSpace(mc.Params["address"])
	hasCoords := strings.TrimSpace(mc.Params["lat"]) != "" || strings.TrimSpace(mc.Params["lon"]) != ""
	switch {
	case address != "" && hasCoords:
		return "", apperr.BadRequest("a map has either coordinates or the address of another map, not both")
	case address != "":
		if tg, err = EmbedTarget(address); err != nil {
			return "", err
		}
	case hasCoords:
		lat, err := coord("lat", mc.Params["lat"], 85)
		if err != nil {
			return "", err
		}
		lon, err := coord("lon", mc.Params["lon"], 180)
		if err != nil {
			return "", err
		}
		zoom := 16
		if z := strings.TrimSpace(mc.Params["zoom"]); z != "" {
			if zoom, err = strconv.Atoi(z); err != nil || zoom < 1 || zoom > 19 {
				return "", apperr.BadRequest("zoom: a whole number from 1 to 19")
			}
		}
		tg = osmTarget(lat, lon, zoom)
	default:
		return "", apperr.BadRequest("a map needs coordinates (lat, lon) or the address of another map")
	}
	if err := allowedEmbed(m.allow, tg); err != nil {
		return "", err
	}
	title := tr(mc.Lang, "Map: ", "Karte: ") + place
	where := `<span class="ewl-map-place">` + esc(place) + `</span>`
	return embedHTML(mc.Lang, tg, title, "", "ewl-map ewl-embed--"+oneOf(mc.Params["ratio"], embedRatios), where), nil
}
