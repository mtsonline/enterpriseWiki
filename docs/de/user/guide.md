# Benutzerhandbuch

## Aufbau

- **Kategorien** gliedern das Wiki wie ein Inhaltsverzeichnis. Sie können beliebig viele Unterkategorien haben und tragen eine Gliederungsnummer (z. B. `1.2.3`).
- **Elemente** liegen in Kategorien. Der Standardtyp ist die **Seite** (formatierter Text oder Markdown). Weitere Typen kommen über Plugins hinzu, z. B. die Hinweisbox.
- Jede Kategorie kann eine **Standardseite** haben. Sie wird beim Öffnen der Kategorie direkt angezeigt.

Links sehen Sie den **Seitenbaum**. Mit den Pfeil-Knöpfen klappen Sie Unterkategorien auf und zu. Oben finden Sie Suche, Sprachwahl, den Knopf **Bearbeiten** und Ihr Benutzermenü.

## Lesen und Sprache

Wählen Sie oben die **Sprache**. Oberfläche und Inhalte wechseln gemeinsam. Gibt es eine Seite noch nicht in Ihrer Sprache, sehen Sie die Originalsprache mit einem Hinweis. Ihre Wahl wird im Browser gespeichert. Unter *Mein Profil* können Sie eine bevorzugte Sprache festlegen.

**Knöpfe, die nur ein Symbol zeigen** (✎, ×, ☰, 🔔 …), nennen ihren Namen, wenn die Maus auf ihnen ruht, wenn sie den Tastaturfokus bekommen und – auf Tablets und Handys – bei langem Drücken (langes Drücken zeigt nur den Namen; kurzes Tippen drückt den Knopf). Escape blendet den Namen aus.

**Auf Handys und kleinen Tablets** (bis 900 Pixel Breite) beginnt die Seite mit ihrem Inhalt: Der Menü-Knopf ☰ oben öffnet Navigation, Suche, Sprache, Bearbeiten und Benutzermenü zusammen mit der Seitenspalte und schließt sie wieder (auch mit Escape oder beim Folgen eines Links).

## Bearbeitungsmodus

Solange der Bearbeitungsmodus aus ist, sieht jede Seite so aus wie für alle Leser:innen. Mit **Bearbeiten** (nur sichtbar, wenn Sie Bearbeitungsrechte haben) schalten Sie ihn ein. Dann erscheinen die Aktionen:

**In einer Kategorie:** *Neue Seite*, *Neue Unterkategorie*, *Kategorie bearbeiten* (Titel, Beschreibung und Tags in allen Sprachen), *Kategorie-Einstellungen* (Standardseite, Adresse, Berechtigungen, Verlauf), *Verschieben*, *Duplizieren*, *Löschen* sowie Pfeile zum Sortieren der Unterkategorien. Die **Standardseite** der Kategorie (der Text, der direkt in der Kategorie angezeigt wird) hat eigene Schaltflächen *Seite bearbeiten*, *Tags* und *Verlauf*; in der Liste *Seiten* führt *Bearbeiten* direkt in den Editor.

**Auf einer Seite:** *Bearbeiten*, *Verlauf*, *Tags*, *Als Standardseite festlegen*, *Verschieben*, *Duplizieren*, *Löschen* und die Übersicht **Übersetzungen**.

### Entwürfe, Vorschau, Vorlagen und Veröffentlichen
Im Editor speichert **Als Entwurf speichern** den Stand, ohne ihn zu veröffentlichen. Entwürfe sehen nur Sie und die Editoren der Kategorie; sie stehen in der Kategorie unter *Entwürfe*. **Vorschau** zeigt die Seite so, wie Leser:innen sie sehen werden. Gibt es ungespeicherte Änderungen, speichert die Vorschau sie zuerst als Entwurf; Links in der Vorschau öffnen sich in einem neuen Tab, der Editor bleibt, wie er ist. Verlassen Sie den Editor mit ungespeicherten Änderungen (Link, *Abbrechen*, Zurück im Browser), fragt das Wiki: als Entwurf speichern, verwerfen oder weiter bearbeiten. Beim Schließen oder Neuladen des Tabs fragt der Browser. **Als Vorlage speichern** legt den Inhalt als Vorlage ab (für Sie, die Kategorie oder – mit dem Recht dazu – für alle); bei *Neue Seite* wählen Sie eine Vorlage aus. Unter *Vorlagen verwalten* ändern Sie Name und Beschreibung einer Vorlage oder ersetzen Titel, Inhalt und Schlagwörter durch das, was gerade im Editor steht; dort löschen Sie Vorlagen auch.

**Veröffentlichen** übernimmt die Änderung sofort. **Veröffentlichen mit Optionen** fragt nach der Zielkategorie (nur Kategorien, in denen Sie anlegen dürfen), einem Zeitpunkt (geplante Veröffentlichung) und ob Beobachter:innen benachrichtigt werden. Plugins können weitere Optionen ergänzen, z. B. „als Neuigkeit“ in einem Karussell.

**Baustein-Vorlagen:** Markieren Sie Bausteine in einer Seite (Text, Layout- und Wiki-Bausteine mit ihren Optionen) und wählen Sie *Als Baustein-Vorlage speichern*: nur für Sie, für eine Kategorie und darunter, einen Bereich oder für alle (wie Seitenvorlagen). *Baustein-Vorlage einfügen* setzt eine Vorlage an der Schreibmarke ein, auch im Markdown-Modus. Eingefügt ist sie ein Teil der Seite: Spätere Änderungen der Vorlage ändern die Seite nicht. Für Inhalte, die überall gleich bleiben sollen, nehmen Sie den Baustein *Seite einbinden*.

### Neue Unterkategorie
Die aktuell geöffnete Kategorie ist automatisch als übergeordnete Kategorie ausgewählt. Titel und Beschreibung können Sie für jede Sprache eingeben.

### Seiten bearbeiten
Seiten werden als **Markdown** gespeichert (GitHub-Markdown). Es gibt zwei Editoren; welchen Sie verwenden, legen Sie im **Profil** fest (*Editor für Seiten*), sonst gilt die Vorgabe der Site.

**Visueller Editor** (Standard):
- Werkzeugleiste: Absatzformat (Absatz, Überschrift 1–4, Codeblock), fett, kursiv, durchgestrichen, Code, Link, Aufzählung, nummerierte Liste, **Aufgabenliste**, Zitat, Tabelle, Bild, Trennlinie, Makro, Rückgängig/Wiederholen. In einer Tabelle erscheinen zusätzlich Knöpfe für Zeilen und Spalten.
- Markdown beim Tippen: `# ` bis `###### ` → Überschrift, `- ` → Liste, `1. ` → nummerierte Liste, `> ` → Zitat, ` ``` ` → Codeblock, `---` → Trennlinie, `[ ] ` am Anfang eines Listenpunkts → Aufgabe, `**fett**`, `*kursiv*`, `~~durchgestrichen~~`, `` `Code` ``. Eingefügter Markdown-Text wird umgewandelt.
- **Codeblöcke** zeigen ihre Sprache in Farbe (über 25 Sprachen, zum Beispiel Go, Python, JavaScript, SQL, YAML) und haben einen Knopf *Code kopieren*. Sprache und Zeilennummern wählen Sie im Codeblock mit dem Knopf *Sprache und Zeilennummern* oder in Markdown in der ersten Zeile (` ```go lines `). Die Zeilennummern sind nur zum Ansehen: Kopieren und Screenreader liefern den Code allein. Die Farben haben immer mindestens 4,5:1 Kontrast.
- Tastatur: Strg+B/I/E/K (fett, kursiv, Code, Link), Strg+Umschalt+X (durchgestrichen), Strg+Umschalt+7/8/9 (Listen), Strg+Alt+0…4 (Absatz, Überschriften), Strg+Z/Strg+Y. **Alt+F10** springt in die Werkzeugleiste (Pfeiltasten wählen, Escape zurück in den Text). **Tab** rückt in Listen ein und springt in Tabellen zur nächsten Zelle; **Escape, dann Tab** verlässt den Editor immer. **Enter** auf einem ausgewählten Makro oder Bild öffnet dessen Einstellungen.
- **Markdown-Modus** (rechts in der Werkzeugleiste) zeigt den Quelltext; der Knopf heißt dann **Visueller Modus** und wechselt zurück. Der Knopf nennt immer den Modus, zu dem er wechselt.
- **Live-Bearbeitung:** Bearbeiten mehrere Personen dieselbe Seite, sehen alle die Änderungen und Cursor der anderen sofort. Über dem Text steht, wer mitbearbeitet; Kommen und Gehen wird für Screenreader angesagt. Rückgängig betrifft nur die eigenen Änderungen. Speichert jemand, bekommen die anderen einen Hinweis und speichern ohne Konflikt weiter. Jede Person hat eine eigene Kombination aus Farbe und Form (●, ■, ▲, ◆); dieselbe Markierung steht am Cursor und in der Liste der Mitbearbeitenden über dem Text. Live bearbeitet wird in der gerade aktiven Ansicht – visuell oder **Markdown-Quelltext** (auch dort mit den Cursorn der anderen). Wer den Modus wechselt, wechselt für alle; das wird angesagt, der Fokus bleibt bei jeder Person, wo er ist.

**Einfacher Editor:** ein schlankes Eingabefeld (formatiert oder Markdown) ohne Live-Bearbeitung, z. B. für ältere Geräte.

- **Tags** stehen direkt unter dem Inhalt (durch Komma getrennt); sie gelten für alle Sprachen der Seite.
- Geben Sie optional eine **Änderungsnotiz** ein. Sie erscheint im Verlauf.
- Hat jemand die Seite gleichzeitig gespeichert (ohne Live-Bearbeitung), werden Sie gewarnt, statt die fremde Änderung zu überschreiben. Laden Sie dann neu und übernehmen Sie Ihre Änderungen.

### Dateien
Wählen Sie bei *Neue Seite* den Elementtyp **Datei**, laden Sie die Datei hoch und beschreiben Sie sie. PDFs, Bilder und Videos werden direkt angezeigt, andere Dateien stehen zum Herunterladen bereit. Eine neue Datei hochzuladen und zu speichern erzeugt eine neue Version. Frühere Dateien bleiben im Verlauf erhalten. Der Text von PDFs ist über die Suche auffindbar.

### Medienbibliothek
Unter *Benutzermenü → Medien* (`/media`) liegen alle Bilder, Dokumente und anderen Dateien der Site. Jede Datei gehört zu einer Kategorie und hat deren Rechte. Als „Ordner“ für Medien eignen sich **unsichtbare Kategorien** (*Kategorie-Einstellungen → Unsichtbare Kategorie*): Sie erscheinen nicht in Navigation, Menüs, Listen und Suche, haben aber ganz normale Rechte.

Beim **Hochladen** legen Sie fest:
- **Kategorie** – bestimmt die Rechte. Wer eine Seite sehen darf, in der die Datei eingebunden ist, sieht sie dort ebenfalls.
- **Herunterladen erlaubt** – ausgeschaltet kann die Datei nur angesehen werden: kein Download-Knopf, kein direkter Link, das Kontextmenü ist gesperrt. PDFs werden dann seitenweise als Bilder gezeigt. (Bildschirmfotos lassen sich technisch nie verhindern – dafür gibt es Wasserzeichen.)
- **Wasserzeichen** – keins; *Original nicht speichern* (nur die Fassung mit Ihrem Namen); *mit Ihrem Wasserzeichen anzeigen, Original zum Herunterladen*; *Namen des Betrachters beim Anzeigen einbrennen*; *beim Anzeigen und Herunterladen*. Name und Datum sind vorausgewählt, beides abwählbar, dazu ein optionaler Kommentar. Gilt für Bilder und PDFs.
- **Eigene Rechte** – ersetzen die Rechte der Kategorie und der Seiten, die die Datei zeigen (Ansehen, Herunterladen, Bearbeiten, Verwalten für Personen, Gruppen, Rollen, angemeldete Benutzer oder Gäste). Sie können nur Rechte vergeben, die Sie selbst haben.

In der **Detailansicht** sehen Sie Größe, Versionen und auf welchen Seiten die Datei verwendet wird; mit Bearbeitungsrecht ändern Sie Texte, laden eine neue Version hoch oder öffnen den **Bildeditor** (Zuschneiden, Drehen, Spiegeln, Skalieren, Helligkeit/Kontrast/Sättigung, Graustufen, Weichzeichnen). Jede Bearbeitung ist eine neue Version; alle Seiten zeigen automatisch die neueste, frühere Versionen lassen sich wiederherstellen.

**Office-Dateien** (Texte, Tabellen, Präsentationen) öffnen sich, wenn die Administration ein Office-Programm
eingerichtet hat (Collabora, OnlyOffice oder Microsoft 365), mit *In … bearbeiten* oder *In … ansehen* direkt im
Browser. Mit Bearbeitungsrecht wird jedes Speichern eine neue Version; ohne Download-Recht sehen Sie die Datei
schreibgeschützt, ohne Herunterladen, Drucken und Kopieren. Ist „immer direkt öffnen“ eingestellt, startet ein Klick
auf die Datei das Programm sofort. *Schließen* führt zurück ins Wiki.

### Bilder in Seiten
Im Editor öffnet der Knopf **Bild einfügen** die Medienbibliothek: Bild auswählen oder neu hochladen (Standard: Kategorie der Seite) und einen Alternativtext angeben, der das Bild für Menschen beschreibt, die es nicht sehen können. Andere Dateien werden als Link eingefügt. Das Makro **Bildergalerie** zeigt ausgewählte Bilder (`media="12,13,14"`) oder alle Bilder einer Kategorie als Raster; ein Klick öffnet das Bild groß, mit den Pfeiltasten blättern Sie weiter. Ein Bild, das kleiner gezeigt wird, als es ist, öffnet sich mit einem Klick groß (wie in der Galerie); der Link trägt den Alternativtext.

### Makros
Makros sind Bausteine, die beim Anzeigen der Seite ausgefüllt werden. Im visuellen Editor fügt der Knopf **Makro einfügen** (⚙) eines ein; Makros mit Inhalt (z. B. Hinweis-Box) umschließen markierte Absätze. Ein eingefügtes Makro erscheint als gestrichelter Kasten mit Name, Einstellungen sowie den Knöpfen *Makro bearbeiten* (✎) und *Makro entfernen* (×); Makros im Text (z. B. Status-Label, Textfarbe) als kleiner Baustein. Layout-Bausteine ohne Inhalt (z. B. Knopfgruppe, Zähler, Symbolbox) und Wiki-Bausteine wie Seitenbaum oder Schlagwort-Wolke zeigen im Kasten gleich, wie sie auf der Seite aussehen; das Bild wird neu gezeichnet, wenn Sie die Einstellungen ändern. **Makros mit Teilen** (Akkordeon, Reiter, Spalten, Karten, Raster, Zeitleiste, Inhalts-Slider) beginnen mit zwei Teilen: Die Überschrift eines Teils ist sein Titel, darunter steht der Inhalt. Mit **+ Teil** (bzw. *+ Reiter*, *+ Spalte* …) oben am Makro fügen Sie einen weiteren Teil an; zwischen zwei Teilen steht eine beschriftete Trennlinie (auch mit dem Knopf *Trennlinie* oder `---` einfügbar). Manche Makros haben einen eigenen Knopf in der Werkzeugleiste (◉ Status-Label, **A** Textfarbe und Schrift); der markierte Text wird dabei übernommen.

Im Markdown-Quelltext stehen Makros als Direktiven: `::toc{levels=3}` (ohne Inhalt), `:::panel{type=info title="Achtung"}` … `:::` (mit Inhalt; verschachtelte Makros außen mit mehr Doppelpunkten) und `:status[ERLEDIGT]{color=green}` (im Text).

| Makro | Wirkung |
|---|---|
| Inhaltsverzeichnis | listet die Überschriften der Seite mit Sprungmarken |
| Unterseiten | Unterkategorien und Seiten dieser (oder einer anderen) Kategorie |
| Zuletzt geändert | die zuletzt geänderten Seiten |
| Hinweis-Box | Info, Notiz, Tipp, Warnung oder Erfolg, mit Titel |
| Aufklappbereich | Inhalt, der erst nach einem Klick erscheint |
| Status-Label | farbiges Label wie ERLEDIGT |
| Textfarbe und Schrift | Textfarbe, Hervorhebung, Schriftart und -größe aus der Palette der Site (alle Kombinationen gut lesbar) |
| Spalten / Karten | Inhalt nebeneinander; Spalten bzw. Karten mit einer Trennlinie trennen |
| Abschnitt, Hero-Banner, Button-Link, Abstand | Bausteine für Website-Seiten (besonders mit der Vorlage *CMS*) |
| Raster | Inhalte in Spalten, die Zahl je Ansicht (Desktop bis 5, Tablet bis 3, Handy 1); Zellen mit einer Trennlinie trennen. Mit *Breiten* bekommen die Spalten auf dem Desktop eigene Anteile (`widths="2 1"`: die erste doppelt so breit); die schmalste behält genug Platz |
| Überschrift | eine Überschrift mit Untertitel und Linie; ihre Ebene (die Gliederung der Seite für Screenreader) wird getrennt von ihrer Größe gewählt |
| Trenner | eine Linie (durchgehend, gestrichelt, gepunktet, doppelt, auslaufend) oder ein Abstand zwischen Teilen einer Seite |
| Knopfgruppe | bis zu vier Links als Schaltflächen, nebeneinander, über die volle Breite oder untereinander; jede wahlweise mit einem Symbol vor dem Text (Schmuck: der Text benennt die Schaltfläche) |
| Handlungsaufruf | ein Kasten mit Titel, Text und Schaltfläche, wahlweise mit Symbol |
| Reiter | Teile als Reiter (Pfeiltasten, Pos1, Ende); Teile mit einer Trennlinie trennen, die erste Überschrift jedes Teils benennt seinen Reiter. Ohne Skript stehen sie als Abschnitte untereinander |
| Akkordeon | Teile zum Auf- und Zuklappen, wahlweise immer nur einer offen und der erste offen |
| Fortschrittsbalken | wie weit etwas ist: ein Name, der Wert als Text und ein Balken (Prozent oder *Wert / Höchstwert*); Farben Hauptfarbe, Erfolg, Warnung, Gefahr |
| Zähler | eine große Zahl mit Namen, wahlweise mit Text davor und danach (`€`, `%`, `km`); sie zählt einmal hoch, wenn sie sichtbar wird (1,2 Sekunden, nie bei weniger Bewegung). Screenreader bekommen immer die ganze Zahl |
| Zeitleiste | Einträge der Reihe nach, jeder mit einem Datum oder Schritt als erster Überschrift; trennen Sie sie mit einer Trennlinie. Von oben nach unten oder auf dem Desktop von links nach rechts (dann scrollt sie in sich und braucht einen Namen; auf dem Handy von oben nach unten) |
| Dialog (Knopf) | ein Knopf, der den Inhalt in einem Dialog mit Titel öffnet. Er öffnet sich nie von selbst; Escape oder *Schließen* schließt ihn, der Fokus kehrt zum Knopf zurück. Ohne Skript öffnet sich der Inhalt an Ort und Stelle |
| Hinweisblase | ein Wort in einer Zeile mit einer Erklärung (`:tooltip[VPN]{tip="Virtuelles privates Netz"}`): Sie erscheint beim Zeigen auf das Wort, mit dem Tastatur-Fokus und beim Tippen, bleibt, solange der Zeiger darauf ist, und verschwindet mit Escape. Screenreader lesen sie als Beschreibung des Wortes; ohne Skript steht sie in Klammern |
| Zitat einer Person | was jemand sagt, mit Name, Funktion und wahlweise einem Bild aus dem Wiki (Kundenstimme) |
| Logo-Leiste | Logos nebeneinander, zum Beispiel Partner: Setzen Sie die Bilder aus der Mediathek hinein, jedes mit seinem Namen als Alternativtext und wahlweise als Link; wahlweise in Grau und farbig beim Zeigen oder mit dem Fokus. Sie braucht einen Namen für Screenreader (zum Beispiel *Unsere Partner*) |
| Schlagwort-Wolke | die Schlagwörter eines Bereichs mit seinen Unterkategorien oder des ganzen Wikis; die häufigsten größer (oder als Liste mit der Zahl der Seiten). Jedes Schlagwort führt zur Suche nach seinen Seiten. Es zählen nur Seiten und Kategorien, die die lesende Person sehen darf |
| Änderungen der Seite | die letzten gespeicherten Fassungen dieser oder einer anderen Seite: Datum, wer (Anzeigename) und der Hinweis zur Änderung, mit Link zum ganzen Verlauf. Nur für Personen, die die Seite sehen dürfen |
| Mitwirkende | die Personen, die an dieser oder einer anderen Seite geschrieben haben, die letzten zuerst, mit der Zahl ihrer Fassungen; nur mit ihrem Anzeigenamen |
| Personenkarte | eine Person mit Bild (aus dem Wiki), Name, Funktion, einem kurzen Text und Kontaktwegen (E-Mail, Telefon, ein Link) |
| Überschrift mit wechselnden Wörtern | eine Überschrift, in der ein Wort wechselt (`::rotating{before="Wir bauen" words="Wikis|Websites|Werkzeuge"}`), überblendet oder getippt. Screenreader bekommen die ganze Überschrift mit allen Wörtern; ein Knopf hält sie an; sie hält beim Zeigen und mit dem Fokus an und steht bei weniger Bewegung still |
| Laufband | die Einträge einer Liste laufen durch ein Band; immer mit Pause-Knopf, es hält beim Zeigen und mit dem Fokus an, steht bei weniger Bewegung still, und die Liste bleibt für Screenreader lesbar (einmal) |
| Inhalts-Slider | Teile, einer nach dem anderen (trennen Sie sie mit einer Trennlinie), mit *Zurück*, *Weiter* und einem Knopf je Teil; wahlweise wechselt er alle 7 Sekunden von selbst, mit Pause-Knopf, nie bei weniger Bewegung. Ohne Skript stehen die Teile untereinander |
| Symbol | ein Symbol der mitgelieferten Sammlung (über 6.000, Umriss oder gefüllt), auch in einer Zeile (`:icon[Telefon]{name=phone}`); ausgewählt mit einer Suche. Screenreader lesen seine Beschriftung oder, ohne eine, den Namen des Symbols (auf Englisch, zum Beispiel „arrow right“); nur ein Symbol mit *Nur Schmuck* wird übersprungen |
| Symbolbox | ein Symbol mit Titel und Text; mit Link ist der ganze Kasten ein Link. Das Symbol wird wie ein einzelnes Symbol vorgelesen |
| Symbolliste | eine Liste mit einem Symbol vor jedem Eintrag, zum Beispiel einem Häkchen. Das Symbol ersetzt den Aufzählungspunkt: Screenreader lesen die Liste als Liste, ohne das Symbol |
| Seite einbinden | zeigt eine andere Seite (oder nur ihren Auszug) hier, immer im aktuellen Stand. Jede Person sieht nur, was sie sehen darf; eine Seite, die strenger eingestuft ist als diese, wird hier nicht gezeigt (auch nicht ihr Titel), damit sie nicht mit dieser Seite per E-Mail, Druck oder Export weiterwandert. Eine Seite bindet höchstens `core.content.includes_per_page` Seiten ein, über alle Ebenen gezählt (Experteneinstellung) |
| Auszug | markiert einen Teil dieser Seite, den *Seite einbinden* mit *Nur den Auszug* zeigt; mit Namen gibt es mehrere Auszüge auf einer Seite |
| Anker | eine Stelle im Text, zu der ein Link mit `#name` springt (`:anchor{name=budget}`). Namen: Kleinbuchstaben, Ziffern und Bindestriche, ohne `ew` am Anfang und ohne `-Zahl` am Ende (so heißen Felder des Wikis selbst; ebenso nicht `f` mit Zahl und Bindestrich am Anfang); jeder Name einmal je Seite, auch nicht wie eine Überschrift. Überschriften bekommen ihre Sprungadresse aus dem Text (`#budget`); hieße sie wie eine Adresse des Wikis (zum Beispiel `main`), beginnt sie mit `h-` (`#h-main`). Jede Adresse gibt es auf einer Seite nur einmal, auch mit eingebundenen Seiten (dort mit Nummer, zum Beispiel `#intro-2`) |
| Seitenbaum | Unterkategorien und Seiten einer Kategorie als Baum zum Aufklappen, mit einem Feld zum Filtern; bis zu vier Ebenen |
| Suchfeld | ein Suchfeld für diese (oder eine gewählte) Kategorie mit Unterkategorien oder für das ganze Wiki; die Treffer zeigt die Suchseite, dort lässt sich die Suche auf das ganze Wiki erweitern. Schon beim Tippen erscheinen die ersten Treffer darunter (abschaltbar); Escape schließt die Liste |
| Seite aus Vorlage anlegen | ein Knopf, der eine neue Seite in einer Kategorie öffnet, gefüllt aus einer Seitenvorlage. Nur wer dort Seiten anlegen darf und die Vorlage nutzen kann, sieht ihn; nehmen Sie deshalb eine Vorlage für alle oder für den Bereich |
| Anhänge | die Dateien, die diese Seite zeigt, oder die Dateien einer Kategorie (wahlweise mit Unterkategorien), als Tabelle mit Art, Größe und Datum, sortiert nach Name, Datum, Größe oder Art. Jede Person sieht nur Dateien, die sie sehen darf; Dateien „nur ansehen“ haben keinen Download. Wer in der Kategorie Seiten anlegen oder bearbeiten darf, lädt mit *Datei hochladen* direkt dort eine Datei hoch |
| Shell-Befehl | Ausgabe eines Befehls auf dem Server; läuft erst, wenn ein Superadmin genau diesen Befehl freigegeben hat |
| HTTP-Abruf | Ergebnis einer Web-Adresse (Text, HTML, JSON als Tabelle); nur freigegebene Adressen |
| Neuigkeiten aus mehreren Quellen | die neuesten Elemente beliebig vieler Quellen und Einträge externer RSS/Atom-Feeds gemeinsam, als Karten, Liste, Schlagzeilen oder Kacheln (Bilder in Spalten je Ansicht, wahlweise als Mauerwerk); mit *Filter nach Schlagwort* stehen Knöpfe über der Liste, die nur Einträge mit einem Schlagwort zeigen |
| Blogbeiträge aus mehreren Quellen | Beiträge mehrerer Quellen im Aussehen des Blogs (Liste, Karten, Kompakt) |
| FAQ aus mehreren Quellen | Fragen mehrerer Quellen als eine FAQ, ein aufklappbarer Abschnitt je Kategorie |
| Zuletzt geändert, Nächste Termine | ebenfalls mit frei wählbaren Quellen |

**Quellen** stellen Sie in Makros, Blog, FAQ, Kalender, Karussell und Eigenschaften-Berichten gleich mit dem **Quellen-Baukasten** zusammen: Links stehen die Bausteine, ein Klick (oder Enter) fügt einen unter **Elemente aus** hinzu, mit der Maus lassen sie sich auch in einen Bereich ziehen. **Elemente aus** – eine Quelle genügt; **Ausschließen** – Elemente, auf die eine davon zutrifft, erscheinen nie (z. B. Schlagwort „intern“); **Muss auch passen** – nur Elemente, auf die zusätzlich jede davon zutrifft. Jeder Eintrag lässt sich über sein Auswahlfeld (oder per Ziehen am Griff ⠿) in einen anderen Bereich verschieben. Darunter zeigt die Vorschau, wie viele Elemente mit Ihren Rechten herauskommen. Bausteine sind eine **Kategorie** (mit oder ohne Unterkategorien), ein **Schlagwort**, einen **Elementtyp**, **einzelne Elemente**, eine **Suche** (alle Treffer zu Suchbegriffen), einen **Eigenschaften-Filter** (z. B. `status = open; due <= today+7d` – alle Elemente, deren Eigenschaften passen), je nach Stelle auch einen **externen Feed** (RSS/Atom), einen **anderen Kalender** oder ein **iCal-Abonnement**. Im Markdown-Quelltext stehen Quellen durch Strichpunkte getrennt, z. B. `::news{source="category:12+sub; tag:hr; feed:https://example.org/rss"}`; die Filter einer Eigenschaften-Quelle stehen dort durch `&&` getrennt (`props:status = open && due <= today+7d`). Ausschließen steht dort mit `-`, „muss auch passen“ mit `+` vor der Quelle (`category:12+sub; -tag:intern; +props:status = open`). Ohne Quelle – oder nur mit Ausschlüssen – gilt die aktuelle Kategorie mit Unterkategorien.

**Höchstzahl und Seiten:** Bei Listen aus Quellen (Neuigkeiten, Blog, FAQ) wählen Sie die **Höchstzahl Einträge** und ob die Liste **auf Seiten aufgeteilt** wird – mit wie vielen **Einträgen pro Seite**. Geblättert wird ohne Neuladen, der Fokus springt an den Anfang der Liste; jede Seite hat einen eigenen Link. Die Obergrenze legt die Administration fest (Vorgabe 1000).

Jeder sieht in Makros nur, was er auch sonst sehen darf.

### Abläufe (Workflows)
Wählen Sie bei *Neue Seite* den Elementtyp **Workflow**. Mit *Vorlage* übernehmen Sie einen fertigen Ablauf (z. B. *Personal: Bewerbung*) und passen ihn an:

- **Startformular:** Felder, die beim Start ausgefüllt werden (Text, E-Mail, Datum, Auswahl, Datei, Bestätigung …).
- **Schritte:** Jeder Schritt hat eine Art:

| Art | Wirkung |
|---|---|
| Aufgabe | Zuständige (startende Person, Benutzer, Gruppe oder Rolle) entscheiden mit einer der **Aktionen** (z. B. *Zum Gespräch einladen*, *Absagen*); jede Aktion führt zu einem nächsten Schritt und kann eine Notiz verlangen |
| Freigabe | Vier-Augen-Prinzip: eine bestimmte Zahl von Freigaben oder alle Mitglieder einer Gruppe/Rolle müssen freigeben; eine Ablehnung (mit Begründung) beendet die Freigabe |
| Bedingung | prüft Angaben aus dem Formular automatisch (z. B. „Betrag größer als 1000“, „Stelle ist Softwareentwicklung“) und verzweigt |
| E-Mail senden | verschickt automatisch eine vorbereitete E-Mail (Empfänger, Cc, Absender, Antwortadresse, Betreff, Text) |
| Webhook | übergibt den Vorgang an ein anderes System (z. B. Personalsoftware) |
| Wartezeit | wartet Tage/Stunden oder bis zu einem Datum aus dem Formular (z. B. dem Eintrittsdatum) |
| Nachricht | schickt eine Nachricht im Wiki (Glocke) und/oder per E-Mail an `starter`, `actor`, `assignees`, `user:NAME`, `group:NAME`, `role:NAME` oder Adressen |
| Werte setzen | setzt Werte des Vorgangs: Text mit Platzhaltern oder eine Formel (`round(menge * preis, 2)`, `if(betrag > 1000, 1, 0)` …); spätere Schritte nutzen sie als `{{field.NAME}}` und in Bedingungen |
| HTTP-Abfrage | fragt ein anderes System ab (nur Adressen der Freigabeliste `plugins.macros.http_allow`) und übernimmt Werte der JSON-Antwort (Pfad wie `daten.status` oder `einträge.0.id`) |
| Seite anlegen | legt eine Seite (Markdown mit Platzhaltern) in einer Kategorie an und merkt sich ID und Adresse für spätere Schritte |
| Eigenschaften setzen | setzt Eigenschaften einer Seite (z. B. der eben angelegten: `{{field.page}}`) |
| Berechtigungen | vergibt einer Person (Benutzername, Alias, E-Mail oder ID, meist ein Platzhalter wie `{{trigger.data.username}}`) oder Gruppe Lese-, Bearbeitungs- oder Verwaltungsrechte auf eine Kategorie – auf Wunsch nur für **N Tage**, danach werden sie automatisch entzogen – oder entzieht sie |
| Gruppenmitgliedschaft | nimmt eine Person in eine Gruppe auf oder entfernt sie daraus |
| Seite verschieben | verschiebt eine Seite in eine andere Kategorie, z. B. ins Archiv |
| Datei erzeugen | schreibt ein Dokument aus einer Vorlage in die Medienbibliothek: PDF, Word (.docx), Excel (.xlsx), CSV, JSON, Text oder Markdown – Zeilen mit `#`, `##`, `###` werden Überschriften, auf Wunsch mit allen Feldern des Vorgangs (Excel und CSV immer als Tabelle). ID und Adresse der Datei landen in Feldern (`{{field.file}}`) |
| Kalendereintrag | legt einen Termin in einem Kalender an: Titel, Beginn (Datum, Datum mit Uhrzeit, Platzhalter wie `{{field.date}}` oder relativ `+3d`, `+2h`), Dauer, ganztägig, Ort, Zeitzone, Anmeldung. Einladungen schickt ein folgender Nachrichten-Schritt mit der gespeicherten Adresse |
| Weiterer Workflow | startet einen Vorgang eines anderen Workflows mit Werten; wartet auf Wunsch, bis er beendet ist, übernimmt dann Werte und verzweigt bei Misserfolg |
| Parallele Zweige | startet mehrere Zweige gleichzeitig – feste Schritte oder **ein Zweig je Wert** eines Felds (z. B. eine Prüfung je Abteilung, der Wert ist `{{field.item}}`); jeder Zweig endet am **Treffpunkt** (nächster Schritt). Optional reichen *k* von *n* Zweigen, die übrigen werden dann beendet; Werte der Zweige werden übernommen |
| Ende | schließt den Vorgang mit einem Ergebnis (erfolgreich, nicht erfolgreich, neutral) |

Automatische Schritte mit Rechten (HTTP-Abfrage, Seite anlegen, Eigenschaften, Datei erzeugen, Kalendereintrag, Berechtigungen, Gruppenmitgliedschaft, Seite verschieben, weiterer Workflow) handeln mit den Rechten der Person, die den Workflow gespeichert hat (oder der startenden Person) – niemand bekommt über einen Workflow mehr, als diese Person selbst darf.

Bei Aufgaben und Freigaben kann eine **Erinnerung nach X Tagen** eingestellt werden. Zuständige erhalten bei neuen Aufgaben eine E-Mail (abschaltbar im Profil).

**Fristen:** Aufgaben und Freigaben können eine Frist haben – Tage/Stunden ab Erreichen des Schritts oder ab einem Datum des Formulars, auf Wunsch nur **Arbeitstage** (ohne Wochenenden und die Feiertage unter *Administration → Workflow*). Vor Ablauf werden die Zuständigen gewarnt (z. B. 48 und 4 Stunden vorher). Läuft die Frist ab, passiert einmal die gewählte **Eskalation**: nur eine Nachricht, an jemand anderen übergeben, automatisch entscheiden (eine Aktion bzw. Freigabe/Ablehnung) oder den Vorgang abbrechen – jeweils optional mit Nachricht.

**Formular eines Schritts:** Felder mit *Später ausgefüllt* erscheinen nicht im Startformular. Bei Aufgaben und Freigaben legt *Formular dieses Schritts* fest, welche Felder die Zuständigen ausfüllen (optional oder Pflicht), nur sehen oder **nicht sehen** (z. B. das Gehalt). Wer den Vorgang gestartet hat, Verwaltende und Admins sehen immer alles.

**Aufgaben übergeben:** Bei Aufgaben einer Gruppe oder Rolle kann eine Person die Aufgabe **übernehmen** (die anderen sehen sie dann nicht mehr) und **zurückgeben**. Jede zuständige Person kann **weitergeben** (an jemanden, der den Workflow sehen darf) oder eine **Rückfrage an den Antragsteller** stellen: Dieser antwortet mit einer Notiz, danach kommt die Aufgabe zurück. Alles steht im Verlauf.

**Vertretung:** Unter *Meine Aufgaben* tragen Sie für eine Abwesenheit eine Vertretung mit Zeitraum ein. Sie erledigt in dieser Zeit Ihre Aufgaben (auch die Ihrer Gruppen und Rollen) und erhält die Aufgaben-E-Mails; protokolliert wird mit ihrem Namen.

**Entscheiden aus der E-Mail:** Ist im Abschnitt *Optionen* des Workflows *Entscheidungslinks in Aufgaben-E-Mails* an, enthält jede Aufgaben-E-Mail einen Link je Entscheidung (z. B. *Freigeben*, *Ablehnen*). Der Link öffnet den Vorgang mit dieser Entscheidung; er gilt nur für die Person, an die die E-Mail ging, nur für diesen Schritt und einmal. Wie lange er gilt, legt *Links gültig für* fest (Stunden oder Tage, Vorgabe 14 Tage, höchstens 90 Tage); hat der Schritt eine Frist, endet der Link spätestens mit ihr, und sobald der Vorgang weitergeht, ist er ohnehin ungültig. Erlaubt die Administration *Entscheidungen aus E-Mails ohne Anmeldung* (unter *Administration → Workflow*), kann die Entscheidung auch ohne Anmeldung bestätigt werden – nie in vertraulichen oder geheimen Kategorien.

**Signierte Entscheidungen:** Bei Aufgaben und Freigaben kann *Signatur der Entscheidungen* verlangen, dass jede Entscheidung signiert wird – mit einem beliebigen Verfahren, nur qualifiziert oder nur mit einem bestimmten Verfahren; einzelne Aktionen können davon abweichen (z. B. „Nicht zuständig“ ohne Signatur). Beim Entscheiden wählt man das Verfahren (heute: Passkey oder Sicherheitsschlüssel mit PIN oder Biometrie, im Profil einzurichten) und signiert damit genau diese Entscheidung: Aktion, Notiz, Werte des Schrittformulars und alle Daten des Falls in diesem Moment. Im Verlauf steht danach *signiert – prüfen*: Die Prüfung zeigt, ob Datensatz, Signatur der Person und Siegel des Wikis stimmen; der Export (JSON) lässt sich anderswo prüfen, auf der Seite *Signatur prüfen* (`/signatures/verify`) oder mit `ewiki signature verify DATEI`. Entscheidungslinks aus E-Mails führen bei Signaturpflicht immer in den Fall.

**Signierte E-Mails (S/MIME):** Unter *Mein Profil → S/MIME-Zertifikat* können Sie Ihr eigenes S/MIME-Zertifikat hochladen (PKCS#12-Datei mit Passwort), sofern die Administration das erlaubt. E-Mails, die das Wiki mit Ihrer Adresse als Absender verschickt (z. B. aus Workflows), werden dann damit signiert. Das Zertifikat muss eine Ihrer bestätigten Adressen enthalten. Der private Schlüssel liegt dafür verschlüsselt auf dem Server: Das ist ein Nachweis des Absenders, keine elektronische Signatur unter Ihrer alleinigen Kontrolle. Jede Verwendung wird protokolliert; Sie werden vor dem Ablauf benachrichtigt und können das Zertifikat jederzeit entfernen.

**Testlauf:** Wer den Workflow bearbeiten darf, startet mit *Testlauf starten* einen Probedurchlauf. Alle Aufgaben kommen zur testenden Person, nichts verlässt das Wiki (keine E-Mails, Nachrichten, Webhooks, HTTP-Abfragen oder neuen Seiten – im Verlauf als *übersprungen* vermerkt). Testläufe stehen im Filter *Testläufe*, zählen nicht in der Auswertung und werden nach 7 Tagen gelöscht.

**Versionen:** Jeder Vorgang merkt sich die Version des Workflows, mit der er gestartet wurde (im Vorgang angezeigt), und läuft mit ihr zu Ende – auch wenn der Workflow inzwischen geändert wurde.

**Auswertung:** Wer den Workflow bearbeiten oder Vorgänge verwalten darf, sieht unter *Auswertung* offene, abgeschlossene und abgebrochene Vorgänge, Ergebnisse, Durchschnitts- und Median-Dauer, überfällige Vorgänge, Eskalationen, verwendete Versionen und je Schritt, wie viele dort warten und wie lange sie im Schnitt bleiben.

**Aufbewahrung:** Unter *Optionen* legt *Abgeschlossene Fälle aufbewahren (Tage)* fest, wie lange beendete Vorgänge bleiben. Danach werden sie **anonymisiert** (Titel, Daten, Notizen und Personen entfernt; Statistik bleibt) oder **gelöscht**. Leer heißt unbegrenzt.

**Nachrichten je Schritt:** Jeder Schritt kann Nachrichten senden, wenn ein Vorgang ihn erreicht oder verlässt (im Wiki, per E-Mail oder beides, mit Platzhaltern).

**Auslöser** (Abschnitt *Start*): Vorgänge starten nicht nur mit dem Startformular, sondern auch automatisch – auf Wunsch *nur* durch Auslöser:

| Auslöser | Startet einen Vorgang, wenn … |
|---|---|
| Zeitplan | die Zeit kommt: `Minute Stunde Tag Monat Wochentag` (z. B. `0 7 * * 1-5` werktags um 7:00) oder `@daily`, `@weekly`, `@monthly` |
| Änderungen von Seiten | Seiten der gewählten Quellen angelegt, geändert, gelöscht, verschoben, wiederhergestellt werden oder neue Eigenschaften bekommen – optional nur, wenn eine Eigenschaft einen Wert **annimmt** (z. B. *Status* wird *Prüfung*; einmal pro Änderung) |
| Ereignis | ein Kommentar geschrieben, ein Formular abgesendet, sich jemand zu einem Termin an- oder abgemeldet hat oder ein Vorgang eines anderen Workflows endet (Verkettung, optional nur mit einem bestimmten Ergebnis) – auf Wunsch nur in einer Kategorie |
| Konten | eine Person wurde angelegt, deaktiviert, wieder aktiviert oder gelöscht oder in eine Gruppe aufgenommen bzw. daraus entfernt (optional nur eine Gruppe) – egal ob über die Administration, SCIM oder einen Anmeldeanbieter. Nur für Personen, die Konten verwalten (`core.users.manage`); Werte: `{{trigger.data.username}}`, `display_name`, `email`, `group_name` (On- und Offboarding) |
| Eingehender Webhook | ein anderes System eine signierte Anfrage schickt (siehe unten) |

Titel und Werte des Startformulars kommen aus Platzhaltern des Auslösers: `{{trigger.object.title}}`, `{{trigger.object.url}}`, `{{trigger.object.id}}`, `{{trigger.actor}}`, `{{trigger.time}}`, `{{trigger.data.SCHLÜSSEL}}` (Werte des Ereignisses bzw. des JSON eines Webhooks, z. B. `{{trigger.data.bestellung.id}}`). Der Editor zeigt je Auslöser, ob er aktiv ist, die Starts der letzten 24 Stunden, den nächsten Termin und den letzten Fehler.

**Sicherheit:** Automatische Schritte (HTTP, Seite, Eigenschaften, weiterer Workflow) und Auslöser handeln mit den Rechten der Person, die die **Fassung des Workflows gespeichert** hat – nie mit den Rechten eines Admins, der den Workflow nur bearbeiten darf: Speichert er, zählen seine eigenen Rechte. Wahlweise handeln Schritte mit den Rechten der startenden Person und Auslöser von Änderungen und Ereignissen mit denen der Person, die sie ausgelöst hat. Diese Person muss den Workflow und die Seite bzw. das Ereignis sehen dürfen, sonst startet nichts; Quellen werden mit ihren Rechten aufgelöst. Ein Auslöser startet höchstens 60 Vorgänge pro Stunde (Expertenwert `workflow.process.trigger_per_hour`), Ketten von Workflows enden nach 5 Gliedern. Werte, die ein Auslöser in den Vorgang übernimmt, sehen alle, die den Vorgang sehen.

**Eingehender Webhook:** Nach dem Speichern zeigt der Auslöser seine Adresse; *Geheimnis erzeugen* liefert das Geheimnis **einmal** (es ist nicht Teil des Workflows, ein neues macht das alte ungültig). Das andere System sendet `POST` mit JSON und den Headern `X-Ewiki-Timestamp` (Unix-Zeit) und `X-Ewiki-Signature: sha256=<hex HMAC-SHA256(Geheimnis, Zeitstempel + "." + Inhalt)>`, optional `X-Ewiki-Delivery` (eine ID – jede Lieferung startet nur einmal). Anfragen älter als 5 Minuten, mit falscher Signatur oder (falls eingestellt) von nicht erlaubten Absendern (Adressen, Netze, Hostnamen) werden mit `403` abgelehnt; angenommen wird mit `202`. Beispiel:

```sh
BODY='{"bestellung":{"id":"A-17","artikel":"Stuhl"}}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$GEHEIMNIS" -hex | sed 's/^.* //')
curl -X POST "$ADRESSE" -H 'Content-Type: application/json' -H "X-Ewiki-Timestamp: $TS" -H "X-Ewiki-Signature: sha256=$SIG" -d "$BODY"
```

**Platzhalter** in E-Mails, Dateien, Titeln, Kalendereinträgen und allen anderen Textfeldern der Schritte: `{{field.<id>}}` (jedes Feld einzeln, z. B. `{{field.email}}`; auch Werte, die Schritte setzen), `{{label.<id>}}` (Bezeichnung des Felds), `{{all}}` (alle Felder mit Bezeichnung, je Zeile eines), `{{case.title}}`, `{{case.id}}`, `{{case.url}}`, `{{case.step}}`, `{{workflow.title}}`, `{{starter.name}}`, `{{starter.email}}`, `{{actor.name}}` und `{{actor.email}}` (wer den Schritt zuletzt bearbeitet hat), `{{last.action}}`, `{{last.note}}` (Aktion und Notiz dieser Person), `{{site.name}}`, `{{date}}`, `{{case.due}}` (Frist) sowie alle Werte, die Schritte setzen. Im Editor fügt ein Klick auf einen Platzhalter ihn in das zuletzt benutzte Feld ein. Als Empfänger sind außerdem `starter`, `actor`, `user:name`, `group:name` und `role:name` möglich.

Vorlagen: *Personal: Bewerbung* (mit Eingangsbestätigung und Absageschreiben per E-Mail), *Genehmigungsantrag* (Freigabe) und *Einarbeitung* (Wartezeit bis zum ersten Arbeitstag, Willkommens-E-Mail).

Auf der Seite startet *Neuen Vorgang starten* einen Vorgang. Die Tabelle zeigt die Vorgänge, die Sie sehen dürfen; „Sie sind dran“ markiert, wo Sie handeln können. Ein Klick auf den Titel öffnet Angaben, Verlauf und Aktionen. **Meine Aufgaben** (Seitenleiste bzw. `/tasks`) sammelt alles, was auf Sie wartet. Laufende Vorgänge behalten den Ablauf, mit dem sie gestartet wurden, auch wenn die Seite später geändert wird.

### Übersetzen
Im Bearbeitungsmodus zeigt jede Seite, zu wie viel Prozent sie übersetzt ist und für jede Sprache den Status:

| Status | Bedeutung |
|---|---|
| Original | Sprache, in der die Seite angelegt wurde |
| Aktuell | Übersetzung basiert auf der neuesten Version des Originals |
| Veraltet | das Original wurde seit der Übersetzung geändert |
| Fehlt | noch keine Übersetzung |

Mit *Übersetzen* bzw. *Bearbeiten* öffnen Sie den Editor für diese Sprache. Unter *Original anzeigen* sehen Sie den Ausgangstext.

**Kategorien** übersetzen Sie genauso: Im Bearbeitungsmodus zeigt der Knopf **Übersetzungen** (mit dem Recht *Verwalten*) den Status von Titel und Beschreibung je Sprache und den Fortschritt. *Übersetzen* zeigt das Original zum Vergleich; ein leerer Titel entfernt die Übersetzung. Hat sich das Original geändert, ist die Übersetzung *veraltet*, bis Sie sie überarbeiten oder mit *Weiterhin korrekt* bestätigen. Die **Originalsprache** einer Kategorie ist die Sprache, in der Sie sie angelegt haben; ändern lässt sie sich unter *Kategorie bearbeiten*.

### Formulare
Wählen Sie bei *Neue Seite* den Elementtyp **Formular** (Recht *Formulare anlegen*). Der Editor hat links die **Feldtypen** – ein Klick (oder Enter) fügt ein Feld am Ende an, mit der Maus lassen sie sich auch an eine Stelle der Feldliste ziehen. Felder ordnen Sie mit ↑/↓ oder durch Ziehen am Griff ⠿; ein Klick auf ein Feld öffnet seine Einstellungen:

- **Feldtypen:** Text, langer Text, E-Mail, Telefon, Webadresse, Zahl, Datum, Uhrzeit, Datum und Uhrzeit, Auswahlliste, Mehrfachauswahl, Optionen, Kontrollkästchen (eines oder mehrere), Bewertung (Sterne), Schieberegler, **berechneter Wert**, **Wiederholgruppe**, Datei-Upload, **Unterschrift**, Einwilligung, versteckter Wert sowie Überschrift, Textblock, Trennlinie und **Neue Seite**.
- **Neue Seite** teilt das Formular in Schritte: Absendende sehen „Schritt 2 von 3“, *Weiter* prüft die Felder des Schritts, *Zurück* geht ohne Prüfung; nach einem Fehler beim Absenden springt das Formular zum betroffenen Schritt.
- **Berechneter Wert:** eine Formel aus Zahlen, `+ - * /`, Klammern und `round(x, Stellen)`, `min`, `max`, `abs`, `sum` mit den IDs von Feldern darüber (Kontrollkästchen = 1, Auswahl mit Zahlenwerten). Der Wert erscheint beim Tippen und wird beim Absenden auf dem Server neu berechnet.
- **Wiederholgruppe:** Zeilen mit mehreren Feldern (Text, Zahl, Datum, Auswahl, Kontrollkästchen …), z. B. „Gast: Name, Essen“; *hinzufügen*/*entfernen* per Knopf, mindestens/höchstens Zeilen.
- **Unterschrift:** mit Maus, Stift oder Finger zeichnen – oder den Namen eintippen (Tastatur, Screenreader); die gezeichnete Unterschrift wird als PNG gespeichert.
- **Einstellungen je Feld:** Beschriftung, ID (für Platzhalter und Exporte), Hilfetext, Pflichtfeld, Beispiel im Feld, Vorbelegung, Vorausfüllen (Name, E-Mail oder Benutzername der angemeldeten Person, Wert aus der Adresse `?name=`), Mindest-/Höchstlänge, Muster mit Erklärung, Minimum/Maximum/Schrittweite, Automatisch ausfüllen (WCAG 1.3.5) und **Nur zeigen, wenn** ein Feld darüber einen Wert hat (ist, ist nicht, ist leer, ist ausgefüllt, enthält). Versteckte Felder werden weder geprüft noch gesendet.
- **Datei-Upload:** erlaubte Arten (Bilder, PDF, Office, Text/CSV, ZIP, Audio, Video) oder Endungen, Anzahl und Größe je Datei, Kommentar pro Datei, optional zusätzlich in eine Kategorie der Medienbibliothek. Absendende wählen Dateien aus oder ziehen sie auf die Upload-Fläche. Jede Datei wird nach ihrem **Inhalt** geprüft (passt er zur Endung?), Office-Dateien mit **Makros** und PDF-Dateien mit **Skripten, Aktionen oder eingebetteten Dateien** werden abgelehnt (je Feld erlaubbar), ein eingerichteter **Virenscanner** prüft zusätzlich.
- **Anordnung:** *Raster* (empfohlen) – jedes Feld nimmt 1 bis 12 Spalten ein, auf schmalen Bildschirmen untereinander. *Frei platziert* – Felder werden auf einer Fläche gezogen und an der Ecke in der Größe verändert (Tastatur: Pfeiltasten verschieben, mit Umschalt die Größe, mit Alt in 1-Pixel-Schritten; dazu Zahlenfelder). Screenreader und Tastatur folgen immer der Reihenfolge der Feldliste; auf kleinen Bildschirmen gilt das Raster.
- **Absenden:** nur angemeldete Personen, einmal pro Person, Zeitraum (öffnet/schließt), Höchstzahl, Löschen nach Tagen, Kopie an die absendende Person, **Rechenaufgabe gegen Bots** (der Browser rechnet etwa eine Sekunde, kein CAPTCHA), **Ergebnisse öffentlich** (Häufigkeit der Antworten für alle, die das Formular sehen).
- **Was mit einer Einsendung passiert:** im Wiki behalten, **E-Mail** an Adressen, `user:NAME`, `group:NAME` oder `field:ID` (ein E-Mail-Feld) mit Betreff und Text samt Platzhaltern, **Webhook** (JSON, signiert), **Eintrag in einer Eigenschaften-Datenbank** (Felder → Eigenschaften), **Workflow starten** (Felder → Felder des Workflows), **als Datei ablegen** (PDF, JSON, CSV oder Text in einer Kategorie der Medienbibliothek, Dateiname mit Platzhaltern). E-Mails nach außen und Webhooks brauchen das Recht *Einsendungen nach außen senden*. Diese Ziele werden mit den Rechten der Person ausgeführt, die das Formular angelegt hat – fehlt ihr ein Recht inzwischen, wird das Ziel nicht ausgeführt und bei der Einsendung als Fehler angezeigt. Aus vertraulichen Bereichen gehen keine Inhalte per E-Mail oder Webhook hinaus, nur der Link.

**Absenden darf jede Person, die das Formular sehen darf** (auch Gäste, wenn die Kategorie öffentlich ist). Fehler stehen beim Feld und gesammelt oben mit Links zu den Feldern; der Fokus springt dorthin. Gegen automatisches Absenden helfen ein verstecktes Feld, eine Mindestzeit und Grenzen pro Adresse bzw. Konto – ohne CAPTCHA.

**Einsendungen** stehen unter dem Formular, mit Suche und Dateien zum Herunterladen. Dafür gibt es eigene Rechte: *Einsendungen ansehen*, *Einsendungen exportieren* (CSV oder Excel; Werte, die mit `=` beginnen, bleiben Text), *Einsendungen löschen* und *Einsendungen verwalten* (alle drei). Exportieren und Löschen schließen das Ansehen ein. Die Person, die das Formular angelegt hat, sieht und exportiert seine Einsendungen immer; zum Löschen braucht auch sie das Recht. Gelöschte Einsendungen lassen sich nicht wiederherstellen. *Formular-Admin* schließt *Einsendungen verwalten* ein. Ein Link in der E-Mail führt direkt zur Einsendung. Das Makro **Formular-Ergebnisse** (`::form-results{form=ID}`) zeigt die Häufigkeit der Antworten als Tabelle mit Balken (bei öffentlichen Ergebnissen für alle, sonst nur mit *Einsendungen ansehen*) oder mit `show=table` die Einsendungen als Tabelle (nur mit *Einsendungen ansehen*), mit Höchstzahl und Seiten.

### Blog, FAQ, Veranstaltungskalender und Team

Beim Anlegen eines Elements (*Neues Element*) wählen Sie den Typ:

- **Blog**: zeigt die Seiten einer Kategorie (optional mit Unterkategorien) als Beiträge, die neuesten zuerst. Es gibt Anrisstext, Autor und Datum sowie die Layouts Liste, Karten und Kompakt, dazu Blättern, Schlagwort-Filter (Klick auf einen Tag) und eine **Suche nur im Blog**. Die Beiträge sind normale Seiten: **Neuer Beitrag** öffnet den Editor in der Blog-Kategorie, **Neue Unterkategorie** legt z. B. „Veranstaltungen“ an. Im Editor des Blogs wählen Sie die Kategorie aus oder legen sie mit **Neue Kategorie** an. Unter **Weitere Quellen** kommen Beiträge anderer Kategorien, Schlagwörter oder Elementtypen dazu (auch im Feed und in der Suche des Blogs).
- **Gemeinsame Notizen**: eine Seite, auf der alle mit Bearbeitungsrecht direkt in der Ansicht schreiben – gleichzeitig, mit den Cursorn der anderen (Live-Bearbeitung). Gespeichert wird automatisch wenige Sekunden nach einer Änderung (höchstens alle 30 Sekunden eine neue Version, im Verlauf als „Notizen automatisch gespeichert“). Leser:innen sehen die Notizen als normale Seite. Ideal für Besprechungsprotokolle und gemeinsame Aufgabenlisten.
- **FAQ**: Unterkategorien werden zu aufklappbaren Abschnitten, Seiten zu aufklappbaren Fragen (Titel = Frage, Inhalt = Antwort). Das Suchfeld filtert sofort, *Alle aufklappen/zuklappen* hilft beim Lesen. **Neue Frage** legt Frage und Antwort direkt im Dialog an (Markdown), **Neuer Abschnitt** eine Unterkategorie. Jede Frage hat einen eigenen Link (`#q-123`). **Weitere Quellen** fügen Fragen anderer Kategorien als eigene Abschnitte hinzu.
- **Veranstaltungskalender**:
  - Ansichten Monat, Woche und Liste, Kategorien mit Farben, Suche.
  - Wiederkehrende Termine: täglich, wöchentlich an bestimmten Tagen, monatlich am Tag X oder z. B. „am letzten Freitag“, jährlich; mit Ende nach Datum oder Anzahl. Einzelne Termine einer Serie lassen sich absagen oder mit **Nur diesen Termin bearbeiten** ändern (Titel, Ort, Beschreibung, Datum und Uhrzeit). Geänderte Termine sind gekennzeichnet, Anmeldungen bleiben erhalten, und Angemeldete erhalten bei neuer Zeit oder neuem Ort eine E-Mail. **Änderung zurücksetzen** stellt den Termin wie in der Serie wieder her; **Serie bearbeiten** ändert alle Termine.
  - **Anmeldung** mit Platzzahl, Warteliste und Anmeldeschluss. Bestätigungen kommen per E-Mail mit einer **Kalendereinladung**: Outlook, Google oder Apple Kalender tragen den Termin direkt ein, aktualisieren ihn bei Änderungen und entfernen ihn bei Abmeldung oder Absage (je Kalender abschaltbar). Wird ein Platz frei, rückt automatisch die erste Person der Warteliste nach und wird informiert.
  - **Erinnerungen per E-Mail** (z. B. 1 Tag vorher) an Angemeldete und alle, die „Erinnern“ gewählt haben.
  - **In meinen Kalender (.ics)** für einen Termin und **Abonnieren** des ganzen Kalenders in Outlook, Google oder Apple Kalender (persönlicher Link). Organisatoren sehen die Teilnehmenden und können sie als CSV exportieren.
  - **Weitere Quellen**: Termine anderer Kalender (nur solche, die die Lesenden sehen dürfen; geändert werden sie in ihrem Kalender) und **iCal-Abonnements** externer Kalender (z. B. Feiertage; alle 10 Minuten aktualisiert, nur lesend, die Beschreibung als reiner Text). Jeder Termin zeigt, aus welchem Kalender er stammt.
  - Das Makro **Nächste Termine** zeigt kommende Termine auf jeder Seite – aus diesem, aus mehreren Kalendern oder aus iCal-Abonnements.
- **Team**: Name und Beschreibung (Markdown/HTML), eine oder mehrere Gruppen, deren Mitglieder angezeigt werden, und die Auswahl der Angaben: Foto, Funktion, Abteilung, E-Mail, Telefon, Mobil, Büro, „Über mich“, Gruppen oder Attribute aus LDAP/OIDC/SAML. Darstellung als Karten, Liste oder Tabelle, auf Wunsch nach Gruppen gegliedert, mit Filter und CSV-Export. Die Angaben pflegt jede Person unter *Mein Profil → Visitenkarte*, Administratoren unter *Administration → Benutzer*.

### Reaktionen und Favoriten
- **Reaktionen:** Unter Seiten und Kategorien sowie an Kommentaren reagieren Sie mit einem Emoji. *Reaktion hinzufügen* öffnet eine Auswahl gängiger Emojis (Pfeiltasten wählen, Esc schließt); über *Anderes Emoji* geht jedes Emoji. Ein Klick auf eine Reaktion schaltet Ihre eigene ein oder aus; wer reagiert hat, steht im Tooltip und wird von Screenreadern vorgelesen. Der Autor erfährt im Wiki davon (per E-Mail nur, wenn er das im Profil einschaltet).
- **Favoriten:** *Favorit* bei einer Seite oder Kategorie merkt sie vor. Die Liste *Meine Favoriten* steht in der Seitenleiste (oder an der von der Administration gewählten Position) und im Profil, dort auch zum Sortieren und Entfernen.
- **Eigenschaften:** siehe unten.

### Eigenschaften, Datenbanken und Berichte

Jedes Element – Seite, Datei, Blog, Kalender, egal welcher Typ – kann **Eigenschaften** haben: Angaben wie Status, Verantwortliche, Fälligkeit, Budget oder Standort.

- **Bearbeiten:** Im Editor jedes Elements gibt es den Bereich *Eigenschaften* unter den Schlagwörtern; auf der Seite im Bearbeitungsmodus geht es auch schneller mit *Eigenschaften bearbeiten* (ohne neue Version). Pflichtfelder sind mit * markiert, berechnete Werte (laufende Nummer, Formel) setzt das Wiki selbst. *Verlauf* zeigt, wer welchen Wert wann geändert hat.
- **Definierte und freie Eigenschaften:** Die Verwaltung einer Kategorie legt Eigenschaften mit Typ, Auswahlwerten, Vorgabewerten und Rechten fest (*Kategorie-Einstellungen → Eigenschaften*; globale unter *Administration → Eigenschaften*); sie gelten auch für alle Unterkategorien. Wo erlaubt, fügen Sie mit *Eigenschaft hinzufügen* eigene hinzu (Name und Typ).
- **Typen:** Text (ein- und mehrzeilig), E-Mail, Telefon, Link, Farbe, Code/Kennung, Sprache, Land, ganze Zahl, Dezimalzahl, Prozent, Geldbetrag, Zahl mit Einheit, Bewertung (Sterne), Fortschritt, Dauer, Ja/Nein, Datum, Datum mit Uhrzeit, Uhrzeit, Zeitraum, Auswahl, Status (farbig), Mehrfachauswahl, Person, Gruppe, verknüpfte Elemente, Kategorie, Datei, Ort, Version, laufende Nummer und Formel. **Schlagwörter** sind die Standard-Eigenschaft jedes Elements.
- **Eigenschaften-Datenbank** (Elementtyp): alle Elemente einer oder mehrerer Quellen als Tabelle, Karten, Board, Kalender, Zeitleiste oder Diagramm – suchen, filtern, gruppieren, Werte einer Zeile direkt ändern, *Neuer Eintrag*, Download als CSV oder Excel. **Gespeicherte Ansichten** merken Suche, Darstellung, Gruppierung, Filter und Sortierung – für sich selbst oder (mit Bearbeitungsrecht) für alle Lesenden der Datenbank.
- **Makro „Eigenschaften-Bericht“:** im Makro-Dialog Quellen (Kategorien mit/ohne Unterkategorien, Schlagwörter, Elementtypen, einzelne Elemente), Filter mit passenden Bedingungen je Typ (z. B. *Fälligkeit höchstens today+30d*, *Verantwortlich bin ich*), Spalten, Sortierung und Darstellung wählen: **Tabelle** (mit Gruppen, Zwischensummen und Summen), **Liste**, **Karten**, **Board** (Karten per Auswahlfeld oder Ziehen in eine andere Spalte verschieben), **Kalender**, **Zeitleiste**, **Diagramm** (Säulen, Balken, gestapelt, Linie, Fläche, Kreis, Ring, Heatmap), **Kennzahlen**, **Pivot-Tabelle**, **Schlagwort-Wolke**, **Vollständigkeit** (welche Werte fehlen), **Überfällig und bald fällig** und **Orte**. Lesende können eigene Filter setzen, als CSV oder Excel herunterladen und den Bericht **abonnieren** (täglich oder wöchentlich per E-Mail, immer mit den eigenen aktuellen Rechten; vertrauliche Einträge nur mit Titel und Link). Die Darstellung **Verlauf** zeigt aus der Änderungsgeschichte, wie sich eine Auswahl-, Status-, Ja/Nein- oder Zahlen-Eigenschaft Woche für Woche entwickelt hat (z. B. offene und erledigte Aufgaben, Summe der Stunden). Streng vertrauliche Einträge sind in Downloads und E-Mails nie enthalten. Diagramme haben eine Textzusammenfassung und die Daten als Tabelle.
- Alles zeigt nur, was Sie sehen dürfen: Elemente ohne Leserecht und eingeschränkt sichtbare Eigenschaften fehlen auch in Filtern, Summen und Diagrammen.

### Adressen und „Seite nicht gefunden“
Jede Seite und Kategorie hat **je Sprache eine lesbare Adresse** aus ihrem Titel, z. B. `/wiki/unser-team/ueber-mich` und `/wiki/our-team/about-me`; ohne Übersetzung gilt die Adresse der Originalsprache. Alle Sprachadressen funktionieren, die Adresszeile zeigt die Adresse in Ihrer Sprache. Mit **Nur diese Kurz-URL** (bei der Kurz-URL in den Eigenschaften) ist eine Seite nur über ihre Kurz-URL erreichbar. Wird eine Seite umbenannt oder verschoben oder eine Kurz-URL geändert, führen die **alten Adressen weiterhin zum Ziel** (dauerhafte Weiterleitung). Führt eine Adresse ins Leere, zeigt das Wiki, ob die Seite gelöscht wurde (mit Link zum Papierkorb für alle, die wiederherstellen dürfen), und schlägt passende Seiten vor („Meinten Sie?“) – nur solche, die Sie sehen dürfen.

### Kurz-URLs

Elemente und Kategorien können eine **Kurz-URL** bekommen: Seiten über *Eigenschaften* (dort auch das **Vorschaubild**, das beim Teilen eines Links in Teams, Slack oder Outlook erscheint – ohne Angabe das erste Bild der Seite), Kategorien über *Kategorie bearbeiten* bzw. die Einstellungen. Mit `kalender` ist das Element dann unter `https://ihr-wiki/kalender` erreichbar. Unterebenen wie `team/hr` sind möglich. Jede Kurz-URL gibt es nur einmal; Wörter, die das Wiki selbst verwendet (`admin`, `login`, `search`, …), sind gesperrt. Menüs und Übersichten verlinken automatisch die Kurz-URL.

### Startseite und Kategorieübersicht

Unter *Administration → Einstellungen → Startseite* wählen Administratoren, was unter `/` erscheint: die Übersicht der obersten Kategorien, eine Kategorie oder ein Element. Neue Installationen zeigen die Willkommensseite. Für eine Übersicht von Bereichen eignet sich der Elementtyp **Kategorieübersicht**: eine Kategorie oder die oberste Ebene, mit 1–5 Ebenen Unterkategorien, als Karten oder Liste, auf Wunsch mit Beschreibungen und den Seiten jeder Kategorie.

### Darstellung einer Kategorie

Unter *Kategorie-Einstellungen → Darstellung und Inhalte → Darstellung der Seiten* wählen Sie, wie die Kategorieseite ihre Seiten zeigt (dafür braucht es *Verwalten*). Die Wahl gilt auch für die Unterkategorien, bis eine davon eine eigene wählt; *Wie darüber* übernimmt sie wieder von oben.

- **Art:** *Liste* (Titel, Anfang des Textes, Datum), *Karten* (mit dem Vorschaubild der Seite), *Kacheln* (große Bilder), *Baum* (Unterkategorien mit ihren Seiten zum Aufklappen) *Handbuch* (die Seiten als Navigation in der Seitenspalte, auch auf jeder Seite der Kategorie; die gezeigte Seite ist markiert), *Tabelle* (nach jeder Spalte sortierbar, mit Klick oder Tastatur; weitere Spalten wie Eigenschaften wählen Sie darunter, jede Person sieht nur Werte, die sie sehen darf), *Akkordeon* und *Reiter* (die Seiten öffnen sich an Ort und Stelle; bei den Reitern wechseln die Pfeiltasten, Pos1 und Ende zwischen den Seiten), *A–Z* (nach dem Anfangsbuchstaben), *Zeitleiste* (nach dem Datum der Änderung oder des Anlegens, unter dem Monat) oder *Alles untereinander* (die Seiten vollständig, für kurze Seiten). Akkordeon, Reiter und „alles untereinander“ zeigen höchstens `core.categories.full` Seiten auf einmal.
- **Reihenfolge:** wie in der Kategorie geordnet, nach Titel, zuletzt geändert zuerst oder zuletzt angelegt zuerst.
- **Seiten je Seite** (weitere werden geblättert), **auch die Seiten der Unterkategorien** (ohne unsichtbare und archivierte) und **Anfang des Textes zeigen**.

**Kategorieseite anordnen** (im Bearbeitungsmodus oder im selben Reiter der Einstellungen) ordnet genau diese Kategorieseite an, nicht ihre Unterkategorien: Titel, Knöpfe, Beschreibung, Übersicht und Unterkategorien sind Bausteine an den Positionen *Kopf*, *Werkzeugleiste*, *Inhalt*, *Seitenspalte* und *unter dem Inhalt*. Hier wählen Sie auch **hervorgehobene Seiten**: Seiten, die vollständig gezeigt werden, jede an einer Position, zum Beispiel eine Willkommensseite im Inhalt und eine Kontaktseite in der Seitenspalte. Die Standardseite der Kategorie ist die erste hervorgehobene Seite. Jede Person sieht eine hervorgehobene Seite nur, wenn sie sie sehen darf.

### Bereiche
Jede Kategorie der obersten Ebene ist ein **Bereich**. Wer dort das Recht *Verwalten* hat, ist Bereichsverwaltung – ganz ohne Administratorrechte. Unter *Kategorie-Einstellungen → Bereich* stellt sie ein, was einen Bereich ausmacht:

- **Bereichsschlüssel** (z. B. `HR`): Der Bereich ist dann unter der Kurzadresse `/s/HR` erreichbar; Schlüssel sind eindeutig.
- **Als Vorlage für neue Bereiche anbieten** (nur mit dem Recht der Kategorie-Administration), Export und Import.

Unter *Kategorie-Einstellungen → Darstellung und Inhalte* stellt **jede Kategorie** (mit *Verwalten*) ein, was für sie und ihre Unterkategorien gilt, solange darunter nichts Eigenes festgelegt ist; ein Hinweis zeigt, von welcher Kategorie ein Wert geerbt ist:

- **Eigene Navigation**: Ab hier zeigt die Seitenleiste nur die Kategorien darunter, mit Name, Logo und Verknüpfungen oben.
- **Verknüpfungen**: wichtige Seiten oder externe Adressen oben in dieser Navigation.
- **Erlaubte Elementtypen**: Nur diese Typen lassen sich hier anlegen oder hierher verschieben; *Neues Element* bietet nur sie an. Darunter lässt sich die Auswahl nur weiter einschränken.
- **Sprache neuer Elemente**: Standardsprache für neue Seiten.
- **Erlaubte Makros**: Nur diese Makros bietet der Editor hier an; andere werden nicht ausgeführt, sondern als Hinweis gezeigt. Darunter lässt sich die Auswahl nur weiter einschränken.
- **Logo** und **Akzentfarbe**: Logo in der Navigation und im Verzeichnis; die Akzentfarbe gilt für Links und Knöpfe und muss mit Weiß einen Kontrast von mindestens 4,5:1 haben.

Wird eine Kategorie an die oberste Stelle verschoben, wird sie zum Bereich; wird ein Bereich unter eine andere Kategorie verschoben, ist er keiner mehr (der Schlüssel wird frei). Die Einstellungen unter *Darstellung und Inhalte* bleiben in beiden Fällen.

*Alle Bereiche* (`/spaces`, Link unten in der Navigation) listet alle Bereiche, die Sie sehen dürfen, mit Filter. Name und Verknüpfungen eines Bereichs sieht nur, wer seine oberste Kategorie sehen darf.

**Seitenvorlagen eines Bereichs oder einer Kategorie:** Beim *Als Vorlage speichern* wählt, wer die Kategorie verwaltet, „Alle in dieser Kategorie und darunter“ (die Bereichsverwaltung auch „Alle im Bereich …“). Solche Vorlagen werden nur beim Anlegen von Seiten dort und darunter angeboten.

**Archivieren** (*Kategorie-Einstellungen → Allgemein → Archivieren*): Die Kategorie mit allem darunter wird für alle nur noch lesbar – auch für Administratoren –, verschwindet aus Navigation, Übersichten und Suche und zeigt einen Hinweis. Direkte Links funktionieren weiter; archivierte Bereiche stehen unter *Alle Bereiche* in einem eigenen Abschnitt. *Wiederherstellen* macht alles wieder bearbeitbar.

**Bereichsvorlagen:** Kategorie-Administratoren können einen Bereich *als Vorlage für neue Bereiche anbieten*. *Alle Bereiche → Neuer Bereich* legt dann einen Bereich mit Unterkategorien, Rechten, Einstellungen, Website-Vorlage und Seitenvorlagen der Vorlage an – auf Wunsch auch mit den Seiten.

**Rechte eines Bereichs** sind die ganz normalen Rechte seiner obersten Kategorie (*Kategorie-Einstellungen → Berechtigungen*) und erscheinen deshalb auch in *Administration → Rechte* – dort sind Bereiche gekennzeichnet. Wer Rechte als Person, über eine Gruppe oder eine Rolle hat, ist **Mitglied**; *Alle Bereiche → Nur meine Bereiche* zeigt diese.

**Verschieben:** Jede Kategorie kann jederzeit verschoben werden. Wird sie zur obersten Ebene, ist sie ein Bereich (frühere Bereichseinstellungen gelten wieder, der Schlüssel muss neu vergeben werden). Wird ein Bereich unter eine andere Kategorie verschoben, gibt er seinen Schlüssel frei; seine Einstellungen und Seitenvorlagen ruhen, und es gelten die des neuen Bereichs. Verschieben und Kopieren in einen Bereich ist nur möglich, wenn dessen erlaubte Elementtypen passen.

**Zugriff anfordern:** Wer eine Seite oder Kategorie nicht sehen darf, bekommt „Nicht gefunden“ (ob es sie gibt, wird nicht verraten) und kann angemeldet *Zugriff anfordern*. Die Personen, die die Kategorie verwalten, erhalten eine Benachrichtigung und beantworten offene Anfragen unter *Kategorie-Einstellungen → Berechtigungen* mit *Lesen erlauben*, *Bearbeiten erlauben* oder *Ablehnen*; daraus wird eine normale Berechtigungsregel. Bei einer Seite mit *nur eigenen Rechten* geht die Anfrage an alle, die Rechte auf diese Seite vergeben dürfen; sie beantworten sie in den *Rechten* der Seite, und daraus wird eine Regel auf der Seite. Höchstens 10 Anfragen pro Person und Tag.

**Persönlicher Bereich:** Mit der Berechtigung *Persönlichen Bereich anlegen* legt jede Person unter *Alle Bereiche* einen eigenen Bereich an, den nur sie sieht, bis sie ihn über die Berechtigungen teilt.

**Einstufung** (*Kategorie-Einstellungen → Allgemein → Einstufung*): *Öffentlich*, *Intern*, *Vertraulich* oder *Streng vertraulich*; sie gilt für alles darunter, und es gilt immer die strengste Einstufung darüber (eine Unterkategorie kann nur strenger werden). Folgen:

| Einstufung | Wirkung |
|---|---|
| Intern | nie für Gäste – auch wenn eine Regel Gästen Lesen erlaubt; Hinweis „Intern“ auf jeder Seite |
| Vertraulich | zusätzlich nicht in Atom/RSS-Feeds und Kalender-Abos, Beobachtungs-E-Mails nur mit Titel und Link, Hinweis auf jeder Seite; Bilder und PDFs der Medienbibliothek sowie Bilder in Seiten tragen Name der Betrachterin bzw. des Betrachters und Uhrzeit als Wasserzeichen |
| Streng vertraulich | zusätzlich nicht in der allgemeinen Suche (nur bei Suche innerhalb der Kategorie), nicht in externen Suchindizes, kein Export, keine Kopien; Dateien der Medienbibliothek und Datei-Elemente nur ansehen, nicht herunterladen (außer mit Verwaltungsrecht) |

**Schutz von Dateien wählbar:** In den *Kategorie-Einstellungen* stehen unter der Einstufung *Wasserzeichen für Dateien* (*Automatisch* oder *Aus: immer das Original*) und *Herunterladen von Dateien* (*Automatisch* oder *Erlaubt: Original herunterladen*). Ohne Auswahl gilt die Einstellung darüber; eine Unterkategorie kann den Schutz wieder einschalten. Beim *Duplizieren* übernimmt jede kopierte Kategorie ihre Einstufung, ihr Wasserzeichen und ihre Download-Einstellung; die Kopie ist nie schwächer geschützt als das Original.

**Einstellungen einer Datei gehen vor:** Was direkt bei einer Datei der Medienbibliothek eingestellt ist – eigene Berechtigungen, *Nur ansehen* oder ein Wasserzeichen – lässt sich durch Einstellungen der Kategorie oder des Bereichs nicht aufheben. Kategorie und Bereich können nur zusätzlich einschränken. Solche Dateien werden auch nicht über eine Seite, die sie einbettet, ohne diese Einschränkungen ausgeliefert.

**Downloads von Berichten und Datenbanken:** Beim Anlegen bzw. Bearbeiten wählbar: *Sicher* (Standard: Werte mit `=` am Anfang bleiben Text, damit Excel nichts unerwartet ausführt) oder *Original* (solche Werte werden zu Formeln – nur für vertrauenswürdige Inhalte).

**Auswertung** (*Kategorie-Einstellungen → Auswertung*, mit *Verwalten*): Aufrufe pro Tag, meistgelesene Seiten, seit über sechs Monaten nicht geänderte Seiten und Mitwirkende der letzten 7 bis 365 Tage. Aufrufe werden datensparsam nur pro Seite und Tag gezählt – ohne Personen und Adressen – und nach 400 Tagen gelöscht.

**Zusammenfassung eines Bereichs:** *Beobachten* auf der obersten Kategorie mit Unterkategorien und täglicher oder wöchentlicher Zusammenfassung schickt alle Änderungen des Bereichs gesammelt per E-Mail.

**Exportieren und importieren** (*Kategorie-Einstellungen → Bereich*): Der Export ist eine Datei (JSON Lines) mit allen Unterkategorien, Seiten (letzte Fassung jeder Sprache), Einstellungen und Seitenvorlagen. *Alle Bereiche → Bereich importieren* legt daraus einen neuen Bereich an, *Hier importieren* eine Unterkategorie. Rechte und Dateien sind nicht enthalten (sie hängen von der Installation ab); alle Inhalte werden beim Import wie beim Speichern geprüft und bereinigt, nicht übernehmbare Einträge werden aufgelistet.

### Eigene Website-Vorlage je Kategorie
In den *Kategorie-Einstellungen* wählt, wer die Kategorie verwalten darf, unter **Website-Vorlage** eine der aktiven Vorlagen (z. B. *CMS* für einen öffentlichen Bereich innerhalb eines internen Wikis). Sie gilt für die Kategorie, ihre Unterkategorien und alle Seiten darin; eine Unterkategorie kann wieder eine eigene wählen (die nächstgelegene gewinnt). *Übernehmen* verwendet die Vorlage darüber bzw. die der Site. Beim Wechsel zwischen Bereichen wechselt die Vorlage ohne Neuladen; Inhalt, Fokus und Rechte bleiben gleich.

### Karussells
Karussells zeigen an einer Position der Vorlage (z. B. über dem Inhalt) Neuigkeiten, eigene Folien, Einträge externer Feeds und die neuesten Elemente beliebiger **Quellen** (Kategorien, Schlagwörter, Elementtypen – mit Titel, Zusammenfassung und Vorschaubild; jede Person sieht nur, was sie lesen darf). Beim **Veröffentlichen mit Optionen** wählen Sie mit dem Recht *Neuigkeiten in Karussells veröffentlichen* „Als Neuigkeit in …“ und optional ein Enddatum; Neuigkeiten sehen nur Personen, die die Seite sehen dürfen. Mit *‹* und *›* oder den Punkten wechseln Sie die Folie; läuft das Karussell automatisch, hält *Pause* es an (es hält auch an, solange Maus oder Fokus darin sind, und läuft gar nicht, wenn Ihr System reduzierte Bewegung wünscht).

### Menüs

Unter *Administration → Menüs* (Administratoren) bestehen Menüs aus Einträgen mit einem Ziel:

- eine **Kategorie**,
- ein **Element** jeden Typs (Seite, Blog, FAQ, Kalender …),
- ein **freier Link**,
- ein **anderes Menü** als aufklappbares Untermenü (für mehrstufige Menüs),
- eine **Überschrift**, die nur die eingerückten Einträge darunter gruppiert.

Kategorien und Elemente wählen Sie aus den vorhandenen aus: Kategorien über eine filterbare Liste, Elemente über eine Suche nach Titel oder Kurz-URL in allen Kategorien. Die Kategorie dient dabei nur als optionaler Filter. Über **Neue Kategorie** bzw. **Neues Element** legen Sie Ziele direkt an. Beim neuen Element sind nur Typ und Titel nötig. Die vorgeschlagene Ablage-Kategorie bestimmt nur, wer es sehen und bearbeiten darf. Bei Blog und FAQ wird ohne Auswahl einer Quelle automatisch eine Kategorie mit dem Titel angelegt. Bleibt die Beschriftung leer, erscheint der Titel des Ziels in der jeweiligen Sprache. Einträge, die jemand nicht sehen darf, werden ihm nicht angezeigt.

Beim Bearbeiten eines Menüs sehen Sie nur die Liste der Einträge mit Ziel. **Bearbeiten** öffnet genau einen Eintrag (Ziel, Beschriftungen, übergeordneter Eintrag bis zu vier Ebenen), **Nach oben**/**Nach unten** ändern die Reihenfolge, **Eintrag hinzufügen** legt einen neuen an. Erst **Speichern** im Menü-Dialog übernimmt die Änderungen.

Menüs ohne Platz („nur als Untermenü“) können in mehreren Menüs eingehängt werden. In der Kopfzeile öffnen sich Untermenüs als Ausklapp-Menüs (per Maus, Tastatur, Esc schließt), in der Seitenleiste als aufklappbarer Baum, im Fußbereich vollständig.

## Beobachten

Mit **Beobachten** (auf jeder Seite und Kategorie, für angemeldete Personen) erhalten Sie eine E-Mail, wenn sich etwas ändert, bei Kategorien auf Wunsch auch für alle Unterkategorien. Sie wählen, ob die E-Mail **nur den Link** oder **Link und neuen Inhalt** enthält. Mehrere Änderungen kurz hintereinander kommen gesammelt in einer E-Mail. Statt **sofort** können Sie eine **tägliche** oder **wöchentliche Zusammenfassung** wählen; sie kommt zur von der Administration eingestellten Uhrzeit (Standard 7 Uhr, wöchentlich montags). Eigene Änderungen werden nicht gemeldet, und es werden nur Inhalte gemeldet, die Sie sehen dürfen. Ihre Beobachtungen verwalten Sie unter *Mein Profil → Beobachtungen*.

Die Administration kann Ihnen Beobachtungen auch **zuweisen** (z. B. für eine Abteilung). Sie stehen unter *Mein Profil → Zugewiesene Beobachtungen*; optionale können Sie dort abbestellen, verpflichtende nicht.

## Benachrichtigungen, Teilen und @Erwähnungen

Die **Glocke** neben Ihrem Namen zeigt Benachrichtigungen im Wiki, die Zahl ungelesener steht daneben. Ein Klick auf eine Benachrichtigung öffnet das Ziel und markiert sie als gelesen; *Alle als gelesen markieren* leert die Liste der ungelesenen. Das ✕ neben einer Benachrichtigung löscht sie.

- **Teilen** (Knopf auf jeder Seite) weist Personen, Gruppen oder Rollen mit einer optionalen Nachricht auf die Seite hin. Teilen gibt **keine Rechte**: Wer die Seite nicht sehen darf, wird nicht benachrichtigt – *Empfänger prüfen* zeigt das vorher.
- **@Erwähnungen:** Tippen Sie in einer Seite oder einem Kommentar `@` und den Namen; die erwähnte Person erhält eine Benachrichtigung, wenn sie die Seite sehen darf.

Zusätzlich kommt jeweils eine E-Mail, solange Sie sie unter *Mein Profil → E-Mail-Benachrichtigungen* nicht abschalten.

## Feeds (Atom/RSS)

In den *Kategorie-Einstellungen* lässt sich ein **Atom/RSS-Feed** einschalten (nur diese Kategorie oder mit Unterkategorien). Blogs und FAQs haben eigene Feeds. Der Knopf **Feed** zeigt die Adressen für Feed-Reader. Angemeldete Personen bekommen einen **persönlichen Link** mit Schlüssel, damit der Feed-Reader auch geschützte Inhalte lesen kann. Unter *Mein Profil → Feed- und Kalender-Links* erneuern oder löschen Sie den Schlüssel, falls ein Link weitergegeben wurde.

## E-Mail-Benachrichtigungen
Unter *Mein Profil → E-Mail-Benachrichtigungen* legen Sie fest, worüber Sie per E-Mail informiert werden: neue Aufgaben und Erinnerungen, Abschluss Ihrer Vorgänge, Kommentare zu Ihren Seiten und Antworten in Diskussionen sowie Änderungen an beobachteten Inhalten. Voraussetzung ist eine E-Mail-Adresse im Profil. Ist der E-Mail-Versand vom Betrieb abgeschaltet, weist das Profil darauf hin.

## Kommentare
Unter jeder Seite können angemeldete Benutzer kommentieren und auf Kommentare antworten. Eigene Kommentare lassen sich bearbeiten und löschen. Links werden automatisch verlinkt.

## Verlauf und Wiederherstellen

Jede Speicherung erzeugt eine neue Version. Es wird nie etwas überschrieben. Unter **Verlauf** können Sie jede Version ansehen und **wiederherstellen**. Dabei entsteht eine neue Version mit dem alten Inhalt.

Eine ganze Kategorie setzen Sie unter *Kategorie-Einstellungen → Verlauf & Zurücksetzen* auf einen **Zeitpunkt** zurück, wahlweise mit allen Unterkategorien. Titel, Texte und Seiten bekommen den damaligen Stand, später angelegte Seiten kommen in den Papierkorb. Mit **Verschobene und neue Kategorien einbeziehen** (nur zusammen mit allen Unterkategorien) bekommt auch die Struktur den damaligen Stand: Kategorien und Seiten, die seitdem verschoben wurden, kommen an ihren früheren Platz und in ihre frühere Reihenfolge zurück, auch in die Kategorie hinein oder aus ihr heraus; seitdem angelegte Kategorien (auch Kopien) kommen samt ihren Seiten in den Papierkorb. Die Kategorie selbst bleibt, wo sie ist. Gibt es einen früheren Platz nicht mehr, wird nichts geändert und die Meldung nennt ihn; stellen Sie ihn zuerst aus dem Papierkorb wieder her. Auch das lässt sich wieder rückgängig machen.

## Papierkorb

Gelöschte Kategorien (mit allem Inhalt) und Seiten landen im **Papierkorb** (Benutzermenü). Von dort stellen Sie sie wieder her. Endgültig gelöscht wird nur durch Administratoren oder automatisch nach der eingestellten Aufbewahrungszeit.

## Suche und Tags

- Die **Suche** findet Wörter und Wortanfänge in Titeln, Tags und Inhalten aller Sprachen. Treffer sind hervorgehoben.
- **In einer Kategorie suchen:** Ein Suchfeld auf einer Seite (Baustein *Suchfeld*) sucht nur in seiner Kategorie und ihren Unterkategorien. Die Suchseite nennt die Kategorie; ohne den Haken bei *Nur in …* sucht sie im ganzen Wiki.
- **Tags** vergeben Sie bei Kategorien (Einstellungen) und Seiten (*Tags*). Ein Klick auf einen Tag zeigt alle Inhalte mit diesem Tag. Auf der Suchseite gibt es eine Liste beliebter Tags.

**Unsichtbar** (unter *Eigenschaften* einer Seite) nimmt sie aus Listen, Blogs, FAQs, Makros, Feeds und der Suche; über ihren Link bleibt sie für alle erreichbar, die sie lesen dürfen, und Bearbeitende sehen sie in der Kategorie gekennzeichnet. Unsichtbar ist kein Schutz – dafür gibt es Rechte.

**Rechte einer Seite** (Schaltfläche *Rechte* im Bearbeitungsmodus, für alle, die Rechte darauf vergeben dürfen): Regeln für Personen, Gruppen, Rollen, alle Angemeldeten oder Gäste gelten zusätzlich zu den Rechten der Kategorie – zum Beispiel darf eine Person genau diese Seite bearbeiten. Mögliche Rechte: *Ansehen*, *Bearbeiten*, *Löschen* und *Rechte vergeben*; niemand vergibt mehr, als er selbst hat. Mit **Nur eigene Rechte** gelten die Rechte der Kategorie für diese Seite nicht mehr: Nur Personen mit einer Regel sehen sie, außerdem Administratoren und wer die Kategorie über eine Regel verwaltet. So eine Seite erscheint für alle anderen nirgends – nicht in Listen, Suche, Blog, Feeds, Makros, Benachrichtigungen oder Exporten. Die **Einstufung** einer Seite kann strenger sein als die ihrer Kategorie (es gilt die strengste). Wer sich selbst das Recht *Rechte vergeben* nehmen würde, wird daran gehindert. Beim Kopieren einer Seite mit eigenen Rechten werden die Rechte standardmäßig mitkopiert; ohne sie kopieren darf nur, wer Rechte vergeben darf. Verschieben behält die Rechte, der Papierkorb auch.

## Mein Dashboard

Unter *Benutzermenü → Mein Dashboard* (`/dashboard`) sehen Sie Kacheln auf einen Blick: Benachrichtigungen, Ihre Aufgaben, die von Ihnen gestarteten Vorgänge, zuletzt von Ihnen bearbeitete Seiten, Favoriten, neue Einsendungen zu Formularen, deren Einsendungen Sie sehen dürfen, die nächsten Termine, zuletzt geänderte Seiten und beobachtete Seiten. Administratoren sehen zusätzlich den *Zustand des Wikis*: Version und Updates, E-Mail und Warteschlange, fehlgeschlagene Anmeldungen der letzten 24 Stunden, Weiterleitung des Protokolls, Plugins, Lizenz und Datenbank; ein Problem ist mit *Achtung* oder *Fehler* markiert. Plugins können weitere Kacheln mitbringen. Jede Kachel zeigt nur, was Sie sehen dürfen.

- **Mit der Maus:** *Mit der Maus verschieben* einschalten und eine Kachel an eine andere Stelle ziehen. Die Anordnung wird sofort gespeichert.
- **Mit der Tastatur:** *Kacheln anordnen* öffnet eine Liste. Darin wählen Sie für jede Kachel die Position (*ganze Breite* oder *Spalte 1–3*) oder *Nicht anzeigen* und ändern die Reihenfolge mit ↑ ↓. Eine Kachel wird breit, wenn sie an der Position *ganze Breite* steht.
- Ihr Dashboard ordnen Sie immer selbst an, auch wenn die persönliche Anordnung der übrigen Oberfläche ausgeschaltet ist. Die Verwaltung legt die Vorgabe und Pflicht-Kacheln fest, die Sie nicht verschieben können, und wählt, welche Kacheln angeboten werden. Auf kleinen Bildschirmen stehen die Spalten untereinander.
- Wenn die Verwaltung das Dashboard zur Startseite macht, landen Sie nach der Anmeldung und unter `/` dort. Die Startseite des Wikis bleibt unter `/start` erreichbar (Link *Zur Startseite des Wikis* auf dem Dashboard).

## Platzierung

Wo Suche, Sprache, Bearbeitungsmodus, Benachrichtigungen, Benutzermenü, Kategoriebaum und die Bausteine von Plugins erscheinen, legt die Verwaltung unter *Administration → Platzierung* fest: je Baustein die Position der Vorlage, die Reihenfolge (Knöpfe ↑ ↓) oder *Nicht anzeigen*; *Pflicht* verhindert Änderungen weiter unten. Wer eine Kategorie verwaltet, ändert das unter *Kategorie-Einstellungen → Platzierung* für sie und ihre Unterkategorien; was dort nicht geändert ist, wird geerbt. Wenn die Verwaltung es erlaubt, ordnet jede Person die Bausteine unter *Mein Profil → Meine Anordnung* für sich selbst an. Das Benutzermenü bleibt immer erreichbar. Jedes Menü, jedes Karussell und jeder Inhaltsblock ist ein eigener Baustein: Die Position, die man beim Anlegen wählt, ist seine Vorgabe; in der Platzierung lässt er sich für die Website, eine Kategorie oder (wenn erlaubt) für sich selbst verschieben, ausblenden oder an mehreren Stellen zeigen. Die Favoritenliste ist ebenfalls ein Baustein (Vorgabe: Seitenleiste). Auf einer Seite ist der Inhalt selbst ein Baustein an der Position *Seite: Inhalt*, so lassen sich andere Bausteine im Hauptbereich darüber oder darunter legen.

**Optionen eines Bausteins:** Neben jedem platzierten Baustein legt *Optionen* fest, wie er an dieser Position aussieht und wann er gezeigt wird, in Gruppen:
- **Anzeige:** auf Desktop, Tablet oder Handy ausblenden; nur für Angemeldete oder nur für Gäste; ab und bis zu einem Zeitpunkt; nur in bestimmten Sprachen; Drucken, Seitenumbruch, ein Anker für Links (`#name`) und die Reihenfolge.

  Einen Baustein nur manchen zu zeigen, ist eine Regel der Anzeige, kein Recht: Der Baustein lädt seine Daten wie immer mit den Rechten der lesenden Person.
- **Größe und Abstand:** Höchstbreite, Mindesthöhe, Seitenverhältnis, Innen- und Außenabstand und Textausrichtung, wo sinnvoll je Ansicht. Das Tablet übernimmt den Wert des Desktops, das Handy den des Tablets, wenn sie keinen eigenen haben.
- **Hintergrund:** Farbe, Verlauf, Bild des Wikis (Fokus, Größe, Wiederholung, Parallax), Video der Medienbibliothek (ohne Ton, in Schleife, immer mit Knopf zum Anhalten; mit „weniger Bewegung“ startet es nicht von selbst; es braucht wie ein Bild eine Überlagerung), Überlagerung, Mischmodus, Muster, Glas.
- **Rahmen, Form und Trenner:** Rahmen, abgerundete Ecken, Schatten, Leuchten, Trenner oben und unten (Welle, Zickzack, Schräge, Bogen), Ausblenden am Rand.
- **Schrift:** Schrift, Größe, Stärke, Zeilenhöhe, Laufweite, Großbuchstaben, Textfarbe, Spalten, Silbentrennung, ausgewogener Umbruch, Initiale, Textschatten, begrenzte Zeilen mit *Mehr anzeigen*, Überschriften im Verlauf, unterstrichene Links.

Das Wiki prüft jeden Wert; ein Beispiel zeigt das Aussehen sofort:
- ein Baustein mit eigenem Hintergrund braucht eine eigene Textfarbe mit genug Kontrast (4,5:1);
- Text auf einem Bild braucht eine Überlagerung; die Textfarbe muss heller oder dunkler sein als jede Stelle des Bildes darunter (das gilt auch für Verläufe);
- Textspalten brauchen in jeder Ansicht Platz (jede mindestens 12 rem; auf dem Handy eine Spalte).

Links in einem solchen Baustein behalten seine Textfarbe und sind unterstrichen.

Dieselben Optionen hat **jeder Baustein im Inhalt** einer Seite (Abschnitt, Raster, Hinweisbox, Karten …): im Dialog des Bausteins unter *Optionen*. Optionen, die das Wiki nicht annimmt, werden nicht verwendet; der Baustein nennt dann den Grund.

Auch eine Seite besteht aus Bausteinen: Titel, die Knöpfe zum Verwalten, *Beobachten*, *Teilen*, *Favorit*, die Angaben zur Seite (Schlagwörter, Autor, Version), Eigenschaften, Reaktionen und Kommentare. Sie stehen an den Positionen einer Seite: Kopf, Werkzeugleiste, Seitenspalte und unter dem Inhalt. Die Verwaltung ordnet sie für alle Seiten an, unter *Administration → Platzierung → Seiten nach Elementtyp* auch eigens für die Seiten eines Typs (zum Beispiel Blogbeiträge mit Vorschaubild und Titelleiste, Termine ohne Seitenspalte); eine Kategorie ordnet sie für ihre Seiten. Für solche Layouts gibt es auch den Baustein *Vorschaubild der Seite*. Eine einzelne Seite ordnen Sie im Bearbeitungsmodus mit *Seite anordnen* an, zum Beispiel ohne sichtbaren Titel (für Screenreader bleibt er erhalten) oder mit *Teilen* unter dem Inhalt. Dafür brauchen Sie das Recht, die Seite zu bearbeiten; bei einem Blog, einer FAQ und ähnlichen Elementen das Recht, sie zu verwalten. Auch die Bedienelemente von Blog und FAQ sind auf deren Seite Bausteine: Suche, *Neuer Beitrag*, *Neue Kategorie*, *Alle aufklappen / zuklappen*, *Neue Frage*, *Neuer Abschnitt* und der Feed. Ist ein Blog oder eine FAQ in eine andere Seite eingebettet (zum Beispiel als Startseite einer Kategorie), zeigt er seine Bedienelemente selbst. Die Knöpfe zum Verwalten bleiben immer erreichbar. Eine Kopie der Seite übernimmt ihre Anordnung und ihre Beschriftungen.

**Titelleiste:** Pfad und Titel einer Seite oder Kategorie sind eigene Bausteine (*Pfad der Seite*, *Titel der Seite*). Der Baustein *Titelleiste* zeigt beide in einem Band; ist er platziert, entfallen die beiden einzelnen Bausteine, so steht nichts doppelt da. Mit seinen Optionen bekommt die Titelleiste einen Hintergrund (Farbe, Verlauf oder Bild mit geprüftem Kontrast). Platziert für eine Kategorie gilt das auch für ihre Unterkategorien; so hat jeder Bereich seine eigene Titelleiste.

Auch die Seite zum Bearbeiten ist anordenbar (für die Website und je Kategorie): Hinweise, Formatierungsleiste, Schlagwörter, Eigenschaften, Originaltext, Kommentar zur Änderung und die Knöpfe *Veröffentlichen*, *Entwurf*, *Vorschau* … stehen im Kopf, über oder unter dem Inhalt, in einer Seitenspalte oder am Ende der Seite. Titel und Inhalt bleiben immer. Hinweise, Eigenschaften und die Knöpfe zum Speichern lassen sich nicht ausblenden, nur verschieben. Welche **Knöpfe die Formatierungsleiste** zeigt, wählt die Verwaltung für die Website (*Administration → Einstellungen*) und wer eine Kategorie verwaltet für sie und ihre Unterkategorien (*Kategorie-Einstellungen*: wie darüber, alle oder eine Auswahl). Ausgeblendete Formatierungen bleiben mit Tastenkürzeln und Markdown möglich.

## Berechtigungen einer Kategorie

Mit dem Recht *Verwalten* legen Sie unter *Kategorie-Einstellungen → Berechtigungen* fest, wer was darf: Benutzer, Gruppe, Rolle, alle Angemeldeten oder Gäste. Wählen Sie eine Vorlage (**Leser**, **Kategorie-Editor**, **Kategorie-Manager**) oder einzelne Rechte, auch die Rechte von Plugins (z. B. *Workflows anlegen*). Optional gilt eine Regel auch für alle Unterkategorien. Geerbte Regeln werden mit ihrer Herkunft angezeigt. Details: [docs/de/admin/security.md](../admin/security.md#berechtigungen).

**Rechte einer Instanz (Blog, FAQ …):** Ein Blog oder eine FAQ hat eigene Rechte, unabhängig von den Rechten für Seiten in der Kategorie: **verwalten** (Einstellungen, Abschnitte und Rechte), **alle Inhalte bearbeiten** und **Inhalte beitragen** (neue und eigene). Auf der Seite der Instanz öffnet **Rechte** die Regeln; wer die Instanz verwaltet, vergibt sie an Personen, Gruppen, Rollen oder alle Angemeldeten, alle anderen sehen dort ihre eigenen Rechte. Wer die Kategorie verwaltet, hat immer alle Rechte ihrer Instanzen; dieselben Rechte lassen sich auch in den Berechtigungen der Kategorie für alle Instanzen darin vergeben. Niemand vergibt Rechte, die er selbst nicht hat. In archivierten Kategorien gelten keine Instanz-Rechte.

**Beschriftungen einer Instanz:** Wer einen Blog oder eine FAQ verwaltet, ändert unter **Beschriftungen** die Texte ihrer Bedienelemente (zum Beispiel „Neuer Beitrag“ oder „Frage stellen“) – je aktiver Sprache. Ein leeres Feld lässt den üblichen Text gelten.

Mit diesen Rechten schreiben Sie im Blog oder in der FAQ auch ohne Seitenrechte in der Kategorie: **Neuer Beitrag** öffnet den Editor im Auftrag des Blogs. Wer *Inhalte beitragen* darf, ändert und löscht seine eigenen Beiträge, wer *alle Inhalte bearbeiten* darf, alle Beiträge, die über den Blog entstanden sind (andere Seiten in der Kategorie nicht). Neue Abschnitte und die Einstellungen eines Blogs oder einer FAQ brauchen *verwalten*; Seitenrechte in der Kategorie reichen dafür nicht.

## Anmelden

Je nach Einrichtung melden Sie sich mit Benutzername und Passwort an (lokales Konto oder LDAP/Active Directory) oder mit einem Knopf **Anmelden mit …** über den zentralen Login Ihrer Organisation (OpenID Connect oder Shibboleth/SAML). Haben Sie weitere Faktoren eingerichtet (oder verlangt die Administration sie), fragt das Formular danach Schritt für Schritt nach ihnen, z. B. nach dem Code aus Ihrer App; fehlt Ihnen ein verlangter Faktor, richten Sie ihn gleich dort ein. Mit einem **Passkey** melden Sie sich, wo erlaubt, ganz ohne Passwort an.

**Passwort vergessen:** Wenn die Verwaltung das anbietet und Sie ein lokales Konto haben, wählen Sie auf der Anmeldeseite **Passwort vergessen?** und geben Ihre E-Mail-Adresse ein. Sie bekommen einen Link, der eine Stunde gilt. Danach sind Sie überall abgemeldet und melden sich mit dem neuen Passwort an.

**Konto selbst anlegen:** Wenn die Verwaltung das erlaubt, steht auf der Anmeldeseite **Konto anlegen**. Geben Sie Ihre E-Mail-Adresse, Ihren Namen und ein Passwort ein. Sie bekommen eine E-Mail mit einem Link, der 48 Stunden gilt. Erst mit diesem Link wird Ihr Konto angelegt; danach melden Sie sich mit Ihrer E-Mail-Adresse und dem Passwort an. Hat die Adresse schon ein Konto, steht das in der E-Mail.

## Mein Profil

- Anzeigename und bevorzugte Sprache, **E-Mail-Adressen** (mehrere möglich, eine davon die Hauptadresse) und **verknüpfte Anmeldungen**,
- Passwort ändern (lokale Konten), optional einen **Benutzernamen** eintragen und unter **Anmeldefaktoren** die eigene Faktorkette sehen und Faktoren einrichten: Authenticator-App, Code per E-Mail, **Sicherheitsschlüssel** oder **Passkey**, **erlaubte Netze**,
- **Sitzungen**: wo Sie angemeldet sind; einzelne Sitzungen beenden oder „Überall sonst abmelden“,
- **API-Keys** für Skripte: Rechte, Kategorie und Ablaufdatum wählen. Der Schlüssel wird nur einmal angezeigt,
- **SSH-Schlüssel** für die passwortlose Anmeldung an der API,
- **Visitenkarte** (Foto, Funktion, Abteilung, Telefon, Büro, „Über mich“) für Team-Seiten. Wer Ihr Foto sieht (nur Sie, angemeldete Personen oder alle), wählen Sie im Rahmen der Vorgaben der Administration,
- **Beobachtungen**, **zugewiesene Beobachtungen** und **Feed- und Kalender-Links**,
- **E-Mail-Benachrichtigungen** (auch für Erwähnungen und geteilte Seiten).

## Barrierefreiheit

Die Oberfläche ist vollständig per Tastatur bedienbar (Tab, Umschalt+Tab, Enter, Leertaste, Esc schließt Dialoge und Menüs). Mit **Zum Inhalt springen** (erste Tab-Taste) überspringen Sie die Navigation. Alle Bedienelemente sind für Screenreader beschriftet, Statusmeldungen werden angesagt, und nach einem Seitenwechsel springt der Fokus auf die Überschrift.
