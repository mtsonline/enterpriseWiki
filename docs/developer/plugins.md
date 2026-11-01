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
| `plugins/theme_confluence_dark` | Template, das von einem anderen Template erbt |
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

`MacroContext` enthält den `RenderContext` (Element, Kategorie, Sprache, Betrachter, `Service` für Abfragen mit dessen Rechten), die Parameter, den **bereits bereinigten** Inhalt (`Body`) und die Überschriften der Seite (`Headings`). Alles andere, was das Makro ausgibt, muss es selbst maskieren (`html.EscapeString`). Links prüfen Sie mit `core_layout.SafeURL`. Fehler erscheinen als Hinweis auf der Seite, nicht als Fehlerseite. Der Makro-Dialog im Editor erscheint automatisch (`GET /api/v1/macros`).

### Dateien in eigenen Tabellen

Speichert ein Plugin Datei-Hashes außerhalb von Element-Inhalten, meldet es die Spalte an, damit die Aufräumfunktion die Dateien nicht löscht: `host.Files.AddReference("plugin_acme_items", "data")`. Ausliefern mit Rechteprüfung: `host.Files.Get` und `host.Files.Serve` (siehe `plugins/workflow_process`).

### E-Mail, Hintergrundjobs, Übersetzungen im Backend

- `host.Mail.Enqueue(ctx, tx, mail.Message{To, Cc, From, ReplyTo, Subject, Text, Source, Ref})` schreibt eine E-Mail in den Postausgang, am besten in derselben Transaktion wie die auslösende Änderung. Versand, Wiederholungen und der Fall „kein Mailserver“ sind Sache des Kerns. `host.Mail.Users(ctx, tx, ids)` liefert Adresse, Sprache und Benachrichtigungswünsche (`u.Wants("tasks")`).
- `host.T(ctx, lang, "acme.mail.subject", map[string]string{"name": …})` übersetzt Texte aus den Locale-Dateien des Plugins (z. B. für E-Mails in der Sprache des Empfängers).
- `host.RegisterWorker("sync", 30*time.Second, func(ctx) error)` registriert einen Hintergrundjob `<plugin-id>.sync`; er läuft nur, solange das Plugin aktiv ist, im Cluster auf einer Instanz zur selben Zeit. Verwenden Sie trotzdem `FOR UPDATE SKIP LOCKED`.
- `host.RegisterSearchBackend(b)` bindet eine externe Suchmaschine an (`content.SearchBackend`, siehe `plugins/search_external`). Die Tabelle `search_changes` protokolliert jede Änderung am Suchindex.

### Rechte in Plugin-Routen

`host.Content.ElementCategory(ctx, elementID, acl.View)` liefert die Kategorie eines Elements und prüft dabei das Recht (404 statt 403, wenn das Element nicht sichtbar ist). `host.Content.Require(ctx, categoryID, perm)` prüft ein Recht auf einer Kategorie.

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

- Einstellungsseiten: `pluginSettingsForm('acme.kanban', [{ key: 'url', label: t('…') }, …])` erzeugt ein barrierefreies Formular, das in `config.yaml` speichert (Typen `text`, `secret`, `bool`, `number`, `list`, `map`, `select`, `textarea`). `MAPPING_FIELDS()` ergänzt die Gruppen- und Rollen-Zuordnung.
- Dateien hochladen: `uploadFile(file)` liefert `{hash, name, mime, size}`.
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
  extends: 'theme.confluence',      // Layout erben ...
  className: 'acme-theme',          // ... und nur per CSS anpassen
});
```

Ein eigenes Layout liefert `layout: (ctx) => ({ root, main })` und platziert Positionen mit `ctx.position('sidebar')`. Pflicht: Sprunglink zum Inhalt, Landmarken (`header`, `nav`/`aside` mit `aria-label`, `main#main[tabindex=-1]`, `footer`). Farben setzen Sie über die CSS-Variablen `--ew-*` (Kontrast ≥ 4.5:1).

## UI-Texte

`locales/en.json` (Referenz) und weitere Sprachen enthalten flache Schlüssel. Präfixen Sie sie mit der Plugin-ID:

```json
{ "acme.kanban.hello": "Hello {name}" }
```

Im Code: `t('acme.kanban.hello', { name })`. Fehlende Übersetzungen erscheinen im Admin-Bereich mit Fortschrittsanzeige und können dort ergänzt werden.

## Bauen, testen, veröffentlichen

```bash
go run ./tools/genplugins     # Plugin in plugins/all/zz_generated.go aufnehmen
./scripts/build.sh
go test ./plugins/acme_kanban/
EWIKI_TEST_DATABASE_URL=… go test ./internal/server/   # prüft u. a., dass jede API-Route im Frontend genutzt wird
```

Veröffentlichen: ein Git-Repository, dessen Wurzel das Plugin-Verzeichnis ist (`plugin.json` im Root), mit Versions-Tags (`v1.0.0`). Installation: `ewiki plugin install github.com/acme/ewiki-kanban --ref v1.0.0` oder über den Katalog.

Das Go-Paket des Plugins gehört zum Modul `github.com/mtsonline/enterprisewiki`. Imports aus `internal/…` und `pkg/sdk` sind daher erlaubt. Halten Sie sich bevorzugt an `pkg/sdk` und die dort referenzierten Dienste, denn das ist die stabile Schnittstelle.
