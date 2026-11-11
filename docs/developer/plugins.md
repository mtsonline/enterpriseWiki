# Plugins entwickeln

Ein Plugin ist ein Verzeichnis unter `plugins/` mit:

```
plugins/acme_kanban/
  plugin.json          Manifest (Pflicht)
  plugin.go            Go-Paket, registriert sich in init() (Pflicht)
  migrations/0001_*.sql  eigene Tabellen (optional)
  locales/en.json, de.json  UI-Texte (optional, en ist Referenz)
  web/index.ts         Frontend-Teil (optional), web/style.css
  *_test.go            Tests (empfohlen)
```

Vollständige Beispiele im Repository:

| Beispiel | zeigt |
|---|---|
| `plugins/example_notice` | Elementtyp, der von `core.html` erbt, eigene API-Route, Übersetzungen, Frontend-Editor, der den Basis-Editor wiederverwendet |
| `plugins/theme_wiki_dark` | Template, das von einem anderen Template erbt |
| `plugins/core_menu` | Migrationen, CRUD-API mit Admin-Prüfung, Widgets an Template-Positionen, eigene Admin-Seite |
| `plugins/auth_local` | Anmeldeverfahren |

## Manifest (`plugin.json`)

```json
{
  "id": "acme.kanban",
  "name": "Kanban board",
  "version": "1.0.0",
  "description": "Agile board as element type",
  "author": "ACME",
  "repo": "github.com/acme/ewiki-kanban",
  "kinds": ["element"],
  "extends": ["core.html"],
  "frontend": true
}
```

- `id`: eindeutig, Kleinbuchstaben, Punkte (`hersteller.name`). Das Verzeichnis heißt üblicherweise wie die ID mit `_` statt `.`.
- `kinds`: `element`, `auth`, `menu`, `template`, `macro`, `generic` (nur Information für die Oberfläche).
- `extends`: Plugins, auf denen dieses aufbaut. Ein Plugin ist nur aktiv, solange alle Basis-Plugins aktiv sind.
- `required`: kann nicht deaktiviert werden (nur für Kern-Plugins).
- `frontend`: `true`, wenn `web/index.ts` existiert.

## Rechte (Pflicht)

Jedes Plugin, das etwas erlaubt oder verbietet, **deklariert seine Rechte im Manifest**. Nicht deklarierte Schlüssel lehnt der Kern ab (`HasPerm` liefert `false` und schreibt einen Fehler ins Log), damit Administratoren alle Rechte zentral sehen und vergeben können (*Administration → Rollen* und *→ Berechtigungen*, Kategorie-Einstellungen).

```json
"permissions": [
  { "key": "acme.kanban.create", "scope": "category", "label": "Create boards",
    "description": "Create boards and change or delete your own ones.", "presets": ["editor"] },
  { "key": "acme.kanban.admin", "scope": "category", "label": "Board administrator",
    "description": "Change and delete all boards of the category.", "presets": ["manager"],
    "implies": ["acme.kanban.create"] },
  { "key": "acme.kanban.export", "scope": "global", "label": "Export all boards", "presets": ["authenticated"] }
]
```

- `key`: beginnt mit der Plugin-ID (`acme.kanban.…`), Kleinbuchstaben, Punkte. Ein umbenannter Schlüssel ist ein neues Recht – bestehende Zuweisungen gehen verloren.
- `scope`: `category` – wird in Kategorie-Regeln vergeben und vererbt; `global` – wird Rollen zugewiesen (Administration → Rollen).
- `label`, `description`: **nur Englisch** im Manifest. Übersetzungen liefern Sie in den Locale-Dateien als `perm.<key>` und `perm.<key>_help` (siehe *UI-Texte*). `en.json` muss alle Schlüssel enthalten, die andere Sprachen übersetzen (der Test `plugins/all` prüft das).
- `presets`: in welchen Vorlagen das Recht enthalten ist. Kategorie: `reader` ⊂ `editor` ⊂ `manager` (ein Recht im Editor-Preset ist auch im Manager-Preset). Global: `authenticated` = alle angemeldeten Benutzer haben es standardmäßig (in der Administration änderbar).
- `implies`: wer dieses Recht hat, hat auch die genannten (z. B. *admin* → *create*).
- `group` (optional): Überschrift in der Rechte-Auswahl; Standard ist der Plugin-Name.

Die fünf Grundrechte einer Kategorie bleiben im Kern: `view`, `edit`, `create`, `delete` (für **Elemente**) und `manage` (die **Kategorie selbst**: Texte, Einstellungen, Unterkategorien, Berechtigungen). Kategorie-Administratoren (`core.categories.admin`) haben überall `manage`, sehen aber Inhalte nur mit `view`.

**Eigenes oder alles:** Bewährt ist ein Recht zum Anlegen, mit dem man nur *eigene* Objekte ändert und löscht, und ein Admin-Recht für alle Objekte. Ändert ein Admin fremde Objekte, protokollieren Sie die Änderung ausführlich (`audit.Log(ctx, host.DB, action, entityType, id, before, after)`) und benachrichtigen den Ersteller (`host.Notify`). Vorbild: `plugins/workflow_process` (`guard.go`).

Prüfen im Go-Teil:

```go
// global
if err := host.RequirePerm(r, "acme.kanban.export"); err != nil { return nil, err } // 403
if host.HasPerm(ctx, "acme.kanban.export") { … }
// in einer Kategorie (benanntes Recht oder Grundrecht "edit")
if !host.Can(ctx, categoryID, "acme.kanban.create") { return nil, apperr.Forbidden() }
// für einen anderen Benutzer (z. B. vor einer Benachrichtigung)
host.UserCan(ctx, uid, categoryID, acl.View)
// Benutzer aus Benutzern, Gruppen und Rollen auflösen
ids, _ := host.Members(ctx, users, groups, roles)
```

Elementtypen können zusätzlich `content.Guard` implementieren: `CheckCreate(ctx, svc, categoryID)` wird vor dem Anlegen, `CheckChange(ctx, svc, el, oldBody, newBody)` vor dem Speichern, Verschieben, Duplizieren und Löschen aufgerufen (`newBody == nil` bei Löschen/Verschieben). `el.CreatedByID` ist der Ersteller.

Im Frontend: `hasPerm('acme.kanban.export')` für globale Rechte, `el.permissions` / `category.permissions` enthält die Grundrechte und benannten Rechte der aktuellen Kategorie. Die Oberfläche blendet nur aus – entscheidend ist immer die Prüfung im Backend.

Admin-Seiten (`registerAdminPage`) geben mit `permission: 'acme.kanban.export'` an, wer sie sieht; ohne Angabe nur Administratoren.

### Erweiterungspunkte im Frontend

| Funktion | Wofür |
|---|---|
| `registerElementExtension({ id, order, placement: 'top' \| 'bottom', render(el) })` | Bereich unter (oder mit `top` über) dem Inhalt einer Seite, z. B. Kommentare, Seiteneigenschaften |
| `registerCategoryExtension({ id, order, render(category) })` | Bereich unter dem Inhalt einer Kategorie |
| `registerPageAction({ id, order, render(target) })` | Knopf in den Aktionen einer Seite oder Kategorie (Teilen, Favorit) |
| `registerSlot({ id, position, render(position) })` | Widget an einer Position der Vorlage; `position: '*'` für alle, `render` darf ein Promise liefern – `null` lässt die Position leer (wichtig, damit leere Positionen ausgeblendet bleiben) |
| `registerProfileSection({ id, order, render() })` | Abschnitt in „Mein Profil“ |
| `registerPublishOption({ id, render(ctx) })` | Feld im Dialog „Veröffentlichen mit Optionen“; im Backend `host.Content.RegisterPublishOption(id, option)` mit `Apply(ctx, svc, el, value)` |
| `registerReactions(provider)` / `reactionBar(type, id, title)` / `prefetchReactions(type, ids)` | Reaktionsleiste eines Plugins (z. B. `core.reactions`) an eigenen Objekten anzeigen; ohne Plugin liefert `reactionBar` `null` |
| `registerMenuStyle({ id, label, itemFields, render(menu, helpers) })` | eigene Darstellung waagrechter Menüs (Beispiel `core.megamenu`); `itemFields: ['description']` blendet Beschreibungsfelder in der Menüverwaltung ein |

Im Backend liefert `host.Content.Path(ctx, "element"|"category", id)` die lesbare Adresse (`/wiki/…`), `host.Content.Resolve(ctx, path)` löst Adressen, Kurz-URLs und frühere Adressen auf.

### Benachrichtigungen, Veröffentlichen-Optionen, Seitenaktionen

- `notify.RegisterKind("acme.kanban.assigned", "acme.kanban.kind")` deklariert eine Art von Benachrichtigung (Benutzer schalten die E-Mail dazu im Profil ab); `notify.RegisterKindOff(…)` für Arten, deren E-Mails erst eingeschaltet werden müssen (z. B. Reaktionen). Senden: `host.Notify.Send(ctx, userIDs, notify.Message{Kind: …, EntityType: "element", EntityID: id, Link: "/e/12", Text: func(lang string) (string, string) { … }})`. Der Absender selbst wird nie benachrichtigt; prüfen Sie vorher mit `UserCan`, ob der Empfänger das Ziel sehen darf.
- `@Erwähnungen` in eigenen Texten: `core_mentions.Notify(ctx, "acme.card", id, categoryID, title, link, text)` (tut nichts, wenn das Plugin deaktiviert ist).
- `registerPublishOption({ id, render })` fügt dem Dialog *Veröffentlichen mit Optionen* ein Feld hinzu; der Wert kommt im Backend über `content.RegisterPublishOption` an (Beispiel folgt mit dem Karussell in Meilenstein 11).
- `registerPageAction({ id, order, render(target) })` fügt einen Knopf in die Aktionen einer Seite oder Kategorie ein (Beispiel: `plugins/core_share`).

## Go-Teil

```go
package acme_kanban

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

func (p *Plugin) Manifest() sdk.Manifest   { return sdk.ParseManifest(manifest) }
func (p *Plugin) Migrations() fs.FS        { s, _ := fs.Sub(migrations, "migrations"); return s }
func (p *Plugin) Locales() fs.FS           { s, _ := fs.Sub(locales, "locales"); return s }

func (p *Plugin) Setup(host *sdk.Host) error {
	host.RegisterElementType(&BoardType{})
	// -> GET /api/v1/x/acme.kanban/boards/{id}
	host.Handle("GET", "/boards/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var cat int64
		if err := host.DB.QueryRow(r.Context(), `SELECT category_id FROM elements WHERE id=$1`, id).Scan(&cat); err != nil {
			return nil, apperr.NotFound("board")
		}
		if !host.ACL.Can(r.Context(), identity.From(r.Context()), cat, acl.View) {
			return nil, apperr.ErrForbidden
		}
		return map[string]any{"id": id}, nil
	})
	return nil
}
```

Wichtig:

- `Setup` wird beim Start für **jedes** kompilierte Plugin aufgerufen. Alles, was über den `Host` registriert wird (Routen, Elementtypen, Anmeldeverfahren), ist automatisch inaktiv, solange das Plugin deaktiviert ist. Routen antworten dann mit 404.
- **Berechtigungen immer selbst prüfen**: `identity.From(ctx)` liefert den Aufrufer (nil = Gast), `host.ACL.Can(...)` die Kategorie-Rechte, `sdk.RequireAdmin(r)` prüft Administratoren. Die Dienste in `host.Content` prüfen Berechtigungen bereits selbst.
- Handler geben `(Wert, error)` zurück. Der Wert wird als JSON gesendet, `apperr`-Fehler werden mit passendem Status gesendet. Mit `h.NoContent{}` und `h.Created{V: x}` steuern Sie Status 204 und 201.
- Migrationen heißen `NNNN_name.sql`, laufen einmal je Datenbank in einer Transaktion und werden nie verändert. Für Änderungen legen Sie eine neue Datei an. Präfixen Sie Tabellennamen (`plugin_acme_…`).
- Einstellungen: `host.Settings()` liefert den Abschnitt `plugin_settings.<id>` aus `config.yaml`.
- Für Änderungen am Inhalt schreiben Sie mit `audit.Log(...)` ins Protokoll.

### Elementtypen

```go
type ElementType interface {
	TypeID() string                                           // "acme.kanban"
	Labels() map[string]string                                // {"en": "Board", "de": "Board"}
	Normalize(body json.RawMessage) (json.RawMessage, error)  // prüfen + bereinigen vor dem Speichern
	Render(ctx context.Context, rc content.RenderContext, body json.RawMessage) (string, error) // sicheres HTML
	Text(body json.RawMessage) string                         // Klartext für die Volltextsuche
	EmptyBody() json.RawMessage                               // Inhalt eines neuen Elements
}
```

Versionierung, Übersetzungen, Papierkorb, Rechte und Suche übernimmt der Kern für jeden Elementtyp. `Render` muss sicheres HTML liefern. Verwenden Sie `core_html.Policy()` bzw. `core_html.ToHTML()` für Benutzertexte.

### Vererbung

Ein Plugin baut auf einem anderen auf, indem es dessen Typ **einbettet** und nur das Abweichende überschreibt (`plugins/example_notice/plugin.go`):

```go
type Type struct{ core_html.Type }      // erbt Normalize, Text, ...

func (t *Type) TypeID() string { return "example.notice" }
func (t *Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	// eigene Darstellung, nutzt core_html.ToHTML
}
```

Tragen Sie die Basis in `extends` ein.

### Makros

Makros stehen im Seiteninhalt als `<div data-macro="name" data-params='{"key":"value"}'>Inhalt</div>` und werden bei jeder Anzeige mit den Rechten des Betrachters gerendert (innere Makros zuerst).

```go
type Macro interface {
	Name() string                      // "acme.weather"
	Labels() map[string]string         // Name im Makro-Dialog
	Description() map[string]string
	Params() []content.MacroParam      // Felder im Dialog: text, textarea, number, select, bool, url
	HasBody() bool                     // umschließt Inhalt (Boxen, Abschnitte, ...)
	Render(ctx context.Context, mc content.MacroContext) (string, error)
}
host.RegisterMacro(&Weather{})
```

Optionale Schnittstellen:

```go
func (Weather) Inline() bool    { return true } // im Text: :acme.weather[Wien]{days=3}; der Text ist der Parameter "text"
func (Weather) Toolbar() string { return "☀" }  // eigener Knopf in der Werkzeugleiste des Editors (Symbol)
```

In Markdown steht ein Makro als Direktive: `::name{a=b}` (ohne Inhalt), `:::name{a=b}` … `:::` (mit Inhalt) oder `:name[Text]{a=b}` (im Text); gerendert wird daraus der obige Platzhalter.

`MacroContext` enthält den `RenderContext` (Element, Kategorie, Sprache, Betrachter, `Service` für Abfragen mit dessen Rechten), die Parameter, den **bereits bereinigten** Inhalt (`Body`) und die Überschriften der Seite (`Headings`). Alles andere, was das Makro ausgibt, muss es selbst maskieren (`html.EscapeString`). Links prüfen Sie mit `core_layout.SafeURL`. Fehler erscheinen als Hinweis auf der Seite, nicht als Fehlerseite. Der Makro-Dialog im Editor erscheint automatisch (`GET /api/v1/macros`).

### Dateien in eigenen Tabellen

Speichert ein Plugin Datei-Hashes außerhalb von Element-Inhalten, meldet es die Spalte an, damit die Aufräumfunktion die Dateien nicht löscht: `host.Files.AddReference("plugin_acme_items", "data")`. Ausliefern mit Rechteprüfung: `host.Files.Get` und `host.Files.Serve` (siehe `plugins/workflow_process`).

### E-Mail, Hintergrundjobs, Übersetzungen im Backend

- `host.Mail.Enqueue(ctx, tx, mail.Message{To, Cc, From, ReplyTo, Subject, Text, HTML, Source, Ref, Owner})` schreibt eine E-Mail in den Postausgang, am besten in derselben Transaktion wie die auslösende Änderung. Versand pro Empfänger, Wiederholungen ohne Limit bei vorübergehenden Fehlern, Rückläufer und der Fall „kein Mailserver“ sind Sache des Kerns. `Owner` bestimmt, wer bei endgültigen Fehlern informiert wird: 0 = die angemeldete Person, die die Nachricht auslöst, -1 = niemand (z. B. für Erinnerungen). Ist E-Mail abgeschaltet (`host.Mail.Enabled()` = false), wird nichts erzeugt. `host.Mail.Users(ctx, tx, ids)` liefert Adresse, Sprache und Benachrichtigungswünsche (`u.Wants("tasks")`).
- `host.T(ctx, lang, "acme.mail.subject", map[string]string{"name": …})` übersetzt Texte aus den Locale-Dateien des Plugins (z. B. für E-Mails in der Sprache des Empfängers).
- `host.RegisterWorker("sync", 30*time.Second, func(ctx) error)` registriert einen Hintergrundjob `<plugin-id>.sync`; er läuft nur, solange das Plugin aktiv ist, im Cluster auf einer Instanz zur selben Zeit. Verwenden Sie trotzdem `FOR UPDATE SKIP LOCKED`.
- `host.Webhooks.Emit(ctx, webhook.Event{Name: "acme.ticket_closed", CategoryID: cat, Object: webhook.Object{Type: "ticket", ID: id, Title: t, URL: "/e/12#ticket-5"}, Data: map[string]any{…}})` meldet ein Ereignis an die konfigurierten Webhooks (Warteschlange, Wiederholungen und Formate Teams/Slack/JSON übernimmt der Kern). Eigene Ereignisnamen müssen in `webhook.Events` stehen, damit Administratoren sie auswählen können; Texte für Teams/Slack kommen aus `webhook.text.<name>` (Platzhalter: `title`, `actor`, `site` und die Schlüssel aus `Data`).
- `host.RegisterSearchBackend(b)` bindet eine externe Suchmaschine an (`content.SearchBackend`, siehe `plugins/search_external`). Die Tabelle `search_changes` protokolliert jede Änderung am Suchindex.

### Rechte in Plugin-Routen

`host.Content.ElementCategory(ctx, elementID, acl.View)` liefert die Kategorie eines Elements und prüft dabei das Recht (404 statt 403, wenn das Element nicht sichtbar ist). `host.Content.Require(ctx, categoryID, perm)` prüft ein Recht auf einer Kategorie.

### Inhalte auflisten, Feeds, eigene Daten eines Elements

- `host.Content.ElementBody(ctx, id, lang)` liefert Typ, gespeicherten Inhalt und Kategorie eines Elements für alle, die es sehen dürfen. So lesen Elementtypen ihre Einstellungen in eigenen API-Routen.
- `host.Content.Scope(ctx, categoryID, recursive)` liefert die Kategorie und (mit `recursive`) die sichtbaren Unterkategorien. `host.Content.ListIn(ctx, content.ListOptions{Categories, Lang, Order, Tag, Limit, Offset, WithContent, Exclude})` listet deren Elemente mit Titel, Anrisstext, Autor und optional gerendertem Inhalt. `host.Content.SearchIn(ctx, q, tags, lang, limit, scope)` sucht nur in diesen Kategorien.
- Das Paket `internal/feed` schreibt Atom und RSS (`feed.Write` mit ETag und 304) und baut Einträge aus Listen (`feed.FromListing`). Persönliche Feed-Links: `host.Auth.FeedContext(r)` wertet `?token=` aus (nur für Feed- und Kalender-Routen verwenden).
- `pkg/catlist` bündelt, was kategoriebasierte Elementtypen brauchen (Konfiguration mit Kategorie und Einleitung, Rechte im Bereich, Routen zum Anlegen von Einträgen und Unterkategorien, Feed). Beispiele sind `plugins/content_blog` und `plugins/content_faq`.
- Ein Elementtyp kann `content.Preparer` implementieren (`Prepare(ctx, svc, categoryID, lang, title, body)`), um einen Inhalt vor der Prüfung mit den Rechten des Aufrufers zu ergänzen. Blog und FAQ legen so die Quell-Kategorie an, wenn keine gewählt wurde (`catlist.PrepareSource`).
- Speichert ein Elementtyp Daten außerhalb seiner Versionen (z. B. Termine eines Kalenders), meldet er Änderungen mit `host.Content.Touch(ctx, elementID)`. Dann werden Beobachter informiert und der Gäste-Cache geleert. Änderungen über die API (POST/PUT/DELETE) leeren den Cache ohnehin.

### Anmeldeverfahren

```go
type PasswordProvider interface {
	ID() string    // z. B. "ldap" -> Identitäten heißen name@ldap
	Name() string
	Authenticate(ctx context.Context, username, password string) (*auth.ExternalIdentity, error)
}
host.RegisterAuthProvider(&MyProvider{})
```

Für Browser-Verfahren (OAuth, SAML, …) implementieren Sie `auth.RedirectProvider` (`Start`, `Callback`) und registrieren ihn mit `host.RegisterRedirectProvider`. Der Kern kümmert sich um State, Nonce und PKCE-Verifier (serverseitig gespeichert), um die Einmalverwendung, die Sitzung und den Rücksprung. Mit `Configured() bool` blenden Sie ein unkonfiguriertes Verfahren auf der Login-Seite aus. Gruppen und Rollen ordnen Sie mit `auth.MappingFromSettings(settings).Apply(identity)` zu (siehe `plugins/auth_oidc`).

Geben Sie `auth.ErrUnknownUser` zurück, wenn der Benutzer unbekannt ist (dann wird das nächste Verfahren geprüft), und `auth.ErrBadCredentials` bei falschem Passwort. `ExternalIdentity.Groups` wird bei jedem Login als Gruppenmitgliedschaft synchronisiert.

## Frontend-Teil (`web/index.ts`)

```ts
import { api, h, registerEditor, registerSlot, registerAdminPage, registerTemplate, getEditor, t } from '@ewiki/sdk';
import './style.css';

export function register(): void {
  // Editor für den eigenen Elementtyp
  registerEditor('acme.kanban', (container, body, ctx) => {
    const ta = h('textarea', { class: 'ew-input', 'aria-labelledby': ctx.labelId });
    ta.value = JSON.stringify(body);
    container.append(ta);
    return { getBody: () => JSON.parse(ta.value) };
  });

  // Widget an einer Template-Position
  registerSlot({ id: 'acme.kanban.widget', position: 'sidebar', order: 30,
    render: async () => h('p', null, t('acme.kanban.hello')) });

  // Seite im Admin-Bereich (/admin/kanban)
  registerAdminPage({ id: 'kanban', label: () => t('acme.kanban.admin'),
    render: async (el) => { el.append(h('h1', null, t('acme.kanban.admin'))); } });
}
```

- Einstellungsseiten: `pluginSettingsForm('acme.kanban', [{ key: 'url', label: t('…') }, …])` erzeugt ein barrierefreies Formular, das in `config.yaml` speichert (Typen `text`, `secret`, `bool`, `number`, `list`, `map`, `select`, `textarea`, `image` – Upload, gespeichert wird der SHA-256-Hash, ausgeliefert unter `/api/v1/files/{hash}` –, `color` – `#rrggbb`). `MAPPING_FIELDS()` ergänzt die Gruppen- und Rollen-Zuordnung.
- Dateien hochladen: `uploadFile(file)` liefert `{hash, name, mime, size}`.
- Auswahl von Kategorien und Elementen mit „Neu anlegen“: `categorySelect({ selected, perm, noneLabel, allowCreate, onChange })` (`.field(label)` liefert das beschriftete Feld), `elementSelect({ selected, allowCreate, type, onChange })` (Suche über alle Kategorien), `newCategory(parent, done)`, `newElement(category, done)`.
- `watchButton(type, id, title)` (Beobachten), `feedButton(apiPath, title)` (Feed-Dialog mit persönlichem Link) und `feedURL(path, format)` für eigene Feeds bzw. Abos.
- Das SDK (`web/src/sdk.ts`) stellt DOM-Helfer (`h`), API-Client (`api` mit CSRF-Header), Übersetzungen (`t`, `pick`), barrierefreie Bausteine (`field`, `textInput`, `select`, `checkbox`, `button`, `formDialog`, `confirmDialog`, `openDialog`), `toast`/`announce` und `navigate` bereit.
- Erzeugen Sie DOM mit `h()`. Verwenden Sie `innerHTML` nie für Benutzerdaten, sondern `trustedHTML()` nur für HTML, das der Server gerendert und bereinigt hat.
- **Barrierefreiheit:** Jedes Eingabefeld braucht ein Label (`field()`), Icon-Knöpfe ein `ariaLabel`, Dialoge über `formDialog`/`openDialog`. Die CI prüft alle Seiten mit axe-core auf WCAG 2.2 AA.
- Editoren anderer Typen lassen sich mit `getEditor('core.html')` wiederverwenden und erweitern (siehe `example_notice/web/index.ts`).
- **Widgets:** Ein Elementtyp kann im serverseitig gerenderten HTML einen Platzhalter `<div data-ew-widget="acme.board" data-element="42"></div>` ausgeben. `registerWidget('acme.board', async (el) => { … })` macht ihn interaktiv (siehe `workflow_process/web`).
- **Bereiche unter Seiten:** `registerElementExtension({ id, order, render: async (element) => node })`, z. B. Kommentare.
- **Eigene Seiten:** `route('/tasks', ({ main }) => …)` registriert eine Route im Frontend.
- Das Frontend hängt nicht an einem Framework und hat keine Laufzeit-Abhängigkeiten. Halten Sie Plugins ebenso schlank.

### Templates

```ts
registerTemplate({
  id: 'acme.theme',
  extends: 'theme.wiki',      // Layout erben ...
  className: 'acme-theme',          // ... und nur per CSS anpassen
});
```

Ein eigenes Layout liefert `layout: (ctx) => ({ root, main })` und platziert Positionen mit `ctx.position('sidebar')`. Verwenden Sie nach Möglichkeit die üblichen Namen (`top-bar`, `header-nav`, `header-tools`, `sidebar`, `sidebar-bottom`, `content-top`, `content-bottom`, `footer`, `footer-1` … `footer-4`, `hero`), damit Menüs und Blöcke beim Wechsel der Vorlage an passender Stelle bleiben; lesbare Namen kommen aus den Übersetzungen `position.<name>`, der Vorlagenname aus `template.<id>`.

Widgets können an einer festen Position (`registerSlot({ position: 'sidebar', … })`) oder an allen Positionen erscheinen (`position: '*'`, `render(position)` bekommt den Namen, siehe `core_menu` und `core_blocks`). Für Auswahllisten liefert `positionOptions()` die Positionen der aktiven Vorlage, `activeTemplate()` die Vorlage selbst. Pflicht: Sprunglink zum Inhalt, Landmarken (`header`, `nav`/`aside` mit `aria-label`, `main#main[tabindex=-1]`, `footer`). Farben setzen Sie über die CSS-Variablen `--ew-*` (Kontrast ≥ 4.5:1).

## UI-Texte

`locales/en.json` (Referenz) und weitere Sprachen enthalten flache Schlüssel. Präfixen Sie sie mit der Plugin-ID:

```json
{ "acme.kanban.hello": "Hello {name}" }
```

Im Code: `t('acme.kanban.hello', { name })`.

**Pluralformen:** Texte mit Anzahl bekommen je CLDR-Pluralform einen Schlüssel mit Endung `_zero`, `_one`, `_two`, `_few`, `_many` oder `_other` (`_other` ist Pflicht). Englisch und Deutsch brauchen `_one` und `_other`, andere Sprachen ergänzen ihre Formen (Polnisch `_few`, `_many`). Der Parameter `count` (oder `n`) wählt die Form – im Frontend über `Intl.PluralRules`, im Backend über die CLDR-Regeln von `golang.org/x/text`:

```json
{ "acme.kanban.cards_one": "{n} card", "acme.kanban.cards_other": "{n} cards" }
```

```ts
t('acme.kanban.cards', { n: list.length })          // "1 card", "3 cards"
```

```go
host.T(ctx, lang, "acme.kanban.cards", map[string]string{"n": strconv.Itoa(n)})
```

Verwenden Sie diese Endungen nicht für andere Zwecke (`…_other` würde als Pluralform gelesen). Für Ordnungszahlen wie „Schritt {n}“ genügt ein einfacher Schlüssel.

**`en.json` ist Pflicht**, sobald ein Plugin Locale-Dateien mitbringt (sonst startet es nicht); weitere Sprachen dürfen nur Schlüssel enthalten, die es in `en.json` gibt. Kern und mitgelieferte Plugins liefern Englisch und Deutsch. Fehlende Übersetzungen erscheinen im Admin-Bereich mit Fortschrittsanzeige (auch je Plugin) und können dort ergänzt oder als JSON, XLIFF 2.0 oder PO exportiert und wieder importiert werden. Ändern Sie in einem Update einen englischen Text, werden die in der Administration gemachten Übersetzungen dieses Textes als *veraltet* markiert.

## Bauen, testen, veröffentlichen

```bash
go run ./tools/genplugins     # Plugin in plugins/all/zz_generated.go aufnehmen
./scripts/build.sh
go test ./plugins/acme_kanban/
EWIKI_TEST_DATABASE_URL=… go test ./internal/server/   # prüft u. a., dass jede API-Route im Frontend genutzt wird
```

Veröffentlichen: ein Git-Repository, dessen Wurzel das Plugin-Verzeichnis ist (`plugin.json` im Root), mit Versions-Tags (`v1.0.0`). Installation: `ewiki plugin install github.com/acme/ewiki-kanban --ref v1.0.0` oder über den Katalog.

Das Go-Paket des Plugins gehört zum Modul `github.com/mtsonline/enterprisewiki`. Imports aus `internal/…` und `pkg/sdk` sind daher erlaubt. Halten Sie sich bevorzugt an `pkg/sdk` und die dort referenzierten Dienste, denn das ist die stabile Schnittstelle.
