# Benutzerhandbuch

## Aufbau

- **Kategorien** gliedern das Wiki wie ein Inhaltsverzeichnis. Sie können beliebig viele Unterkategorien haben und tragen eine Gliederungsnummer (z. B. `1.2.3`).
- **Elemente** liegen in Kategorien. Der Standardtyp ist die **Seite** (formatierter Text oder Markdown). Weitere Typen kommen über Plugins hinzu, z. B. die Hinweisbox.
- Jede Kategorie kann eine **Standardseite** haben. Sie wird beim Öffnen der Kategorie direkt angezeigt.

Links sehen Sie den **Seitenbaum**. Mit den Pfeil-Knöpfen klappen Sie Unterkategorien auf und zu. Oben finden Sie Suche, Sprachwahl, den Knopf **Bearbeiten** und Ihr Benutzermenü.

## Lesen und Sprache

Wählen Sie oben die **Sprache**. Oberfläche und Inhalte wechseln gemeinsam. Gibt es eine Seite noch nicht in Ihrer Sprache, sehen Sie die Originalsprache mit einem Hinweis. Ihre Wahl wird im Browser gespeichert. Unter *Mein Profil* können Sie eine bevorzugte Sprache festlegen.

## Bearbeitungsmodus

Solange der Bearbeitungsmodus aus ist, sieht jede Seite so aus wie für alle Leser:innen. Mit **Bearbeiten** (nur sichtbar, wenn Sie Bearbeitungsrechte haben) schalten Sie ihn ein. Dann erscheinen die Aktionen:

**In einer Kategorie:** *Neue Seite*, *Neue Unterkategorie*, *Kategorie-Einstellungen*, *Verschieben*, *Duplizieren*, *Löschen* sowie Pfeile zum Sortieren der Unterkategorien.

**Auf einer Seite:** *Bearbeiten*, *Verlauf*, *Tags*, *Als Standardseite festlegen*, *Verschieben*, *Duplizieren*, *Löschen* und die Übersicht **Übersetzungen**.

### Neue Unterkategorie
Die aktuell geöffnete Kategorie ist automatisch als übergeordnete Kategorie ausgewählt. Titel und Beschreibung können Sie für jede Sprache eingeben.

### Seiten bearbeiten
- **Formatierter Text:** Die Werkzeugleiste bietet Absatz, Überschriften, fett, kursiv, unterstrichen, durchgestrichen, Listen, Zitat, Code, Link, Tabelle und Trennlinie. Tastenkürzel: Strg+B / Strg+I / Strg+U, Strg+Z rückgängig.
- **Markdown:** Format auf *Markdown* umstellen. Mit *Vorschau* sehen Sie das Ergebnis.
- Geben Sie optional eine **Änderungsnotiz** ein. Sie erscheint im Verlauf.
- Hat jemand die Seite gleichzeitig gespeichert, werden Sie gewarnt, statt die fremde Änderung zu überschreiben. Laden Sie dann neu und übernehmen Sie Ihre Änderungen.

### Übersetzen
Im Bearbeitungsmodus zeigt jede Seite, zu wie viel Prozent sie übersetzt ist und für jede Sprache den Status:

| Status | Bedeutung |
|---|---|
| Original | Sprache, in der die Seite angelegt wurde |
| Aktuell | Übersetzung basiert auf der neuesten Version des Originals |
| Veraltet | das Original wurde seit der Übersetzung geändert |
| Fehlt | noch keine Übersetzung |

Mit *Übersetzen* bzw. *Bearbeiten* öffnen Sie den Editor für diese Sprache. Unter *Original anzeigen* sehen Sie den Ausgangstext.

## Verlauf und Wiederherstellen

Jede Speicherung erzeugt eine neue Version. Es wird nie etwas überschrieben. Unter **Verlauf** können Sie jede Version ansehen und **wiederherstellen**. Dabei entsteht eine neue Version mit dem alten Inhalt.

Eine ganze Kategorie setzen Sie unter *Kategorie-Einstellungen → Verlauf & Zurücksetzen* auf einen **Zeitpunkt** zurück, wahlweise mit allen Unterkategorien. Titel, Texte und Seiten bekommen den damaligen Stand, später angelegte Seiten kommen in den Papierkorb. Auch das lässt sich wieder rückgängig machen.

## Papierkorb

Gelöschte Kategorien (mit allem Inhalt) und Seiten landen im **Papierkorb** (Benutzermenü). Von dort stellen Sie sie wieder her. Endgültig gelöscht wird nur durch Administratoren oder automatisch nach der eingestellten Aufbewahrungszeit.

## Suche und Tags

- Die **Suche** findet Wörter und Wortanfänge in Titeln, Tags und Inhalten aller Sprachen. Treffer sind hervorgehoben.
- **Tags** vergeben Sie bei Kategorien (Einstellungen) und Seiten (*Tags*). Ein Klick auf einen Tag zeigt alle Inhalte mit diesem Tag. Auf der Suchseite gibt es eine Liste beliebter Tags.

## Berechtigungen einer Kategorie

Mit dem Recht *Berechtigungen verwalten* legen Sie unter *Kategorie-Einstellungen → Berechtigungen* fest, wer was darf: Benutzer, Gruppe, Rolle, alle Angemeldeten oder Gäste, mit den Rechten Ansehen, Bearbeiten, Erstellen, Löschen/Verschieben und Verwalten. Optional gilt eine Regel auch für alle Unterkategorien. Geerbte Regeln werden mit ihrer Herkunft angezeigt. Details: [docs/admin/security.md](../admin/security.md#berechtigungen).

## Mein Profil

- Anzeigename, E-Mail und bevorzugte Sprache,
- Passwort ändern (lokale Konten),
- **API-Keys** für Skripte: Rechte, Kategorie und Ablaufdatum wählen. Der Schlüssel wird nur einmal angezeigt,
- **SSH-Schlüssel** für die passwortlose Anmeldung an der API.

## Barrierefreiheit

Die Oberfläche ist vollständig per Tastatur bedienbar (Tab, Umschalt+Tab, Enter, Leertaste, Esc schließt Dialoge und Menüs). Mit **Zum Inhalt springen** (erste Tab-Taste) überspringen Sie die Navigation. Alle Bedienelemente sind für Screenreader beschriftet, Statusmeldungen werden angesagt, und nach einem Seitenwechsel springt der Fokus auf die Überschrift.
