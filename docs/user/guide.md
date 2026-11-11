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

**In einer Kategorie:** *Neue Seite*, *Neue Unterkategorie*, *Kategorie bearbeiten* (Titel, Beschreibung und Tags in allen Sprachen), *Kategorie-Einstellungen* (Standardseite, Adresse, Berechtigungen, Verlauf), *Verschieben*, *Duplizieren*, *Löschen* sowie Pfeile zum Sortieren der Unterkategorien. Die **Standardseite** der Kategorie (der Text, der direkt in der Kategorie angezeigt wird) hat eigene Schaltflächen *Seite bearbeiten*, *Tags* und *Verlauf*; in der Liste *Seiten* führt *Bearbeiten* direkt in den Editor.

**Auf einer Seite:** *Bearbeiten*, *Verlauf*, *Tags*, *Als Standardseite festlegen*, *Verschieben*, *Duplizieren*, *Löschen* und die Übersicht **Übersetzungen**.

### Entwürfe, Vorschau, Vorlagen und Veröffentlichen
Im Editor speichert **Als Entwurf speichern** den Stand, ohne ihn zu veröffentlichen. Entwürfe sehen nur Sie und die Editoren der Kategorie; sie stehen in der Kategorie unter *Entwürfe*. **Vorschau** zeigt die Seite so, wie Leser:innen sie sehen werden. **Als Vorlage speichern** legt den Inhalt als Vorlage ab (für Sie, die Kategorie oder – mit dem Recht dazu – für alle); bei *Neue Seite* wählen Sie eine Vorlage aus.

**Veröffentlichen** übernimmt die Änderung sofort. **Veröffentlichen mit Optionen** fragt nach der Zielkategorie (nur Kategorien, in denen Sie anlegen dürfen), einem Zeitpunkt (geplante Veröffentlichung) und ob Beobachter:innen benachrichtigt werden. Plugins können weitere Optionen ergänzen, z. B. „als Neuigkeit“ in einem Karussell.

### Neue Unterkategorie
Die aktuell geöffnete Kategorie ist automatisch als übergeordnete Kategorie ausgewählt. Titel und Beschreibung können Sie für jede Sprache eingeben.

### Seiten bearbeiten
Seiten werden als **Markdown** gespeichert (GitHub-Markdown). Es gibt zwei Editoren; welchen Sie verwenden, legen Sie im **Profil** fest (*Editor für Seiten*), sonst gilt die Vorgabe der Site.

**Visueller Editor** (Standard):
- Werkzeugleiste: Absatzformat (Absatz, Überschrift 1–4, Codeblock), fett, kursiv, durchgestrichen, Code, Link, Aufzählung, nummerierte Liste, **Aufgabenliste**, Zitat, Tabelle, Bild, Trennlinie, Makro, Rückgängig/Wiederholen. In einer Tabelle erscheinen zusätzlich Knöpfe für Zeilen und Spalten.
- Markdown beim Tippen: `# ` bis `###### ` → Überschrift, `- ` → Liste, `1. ` → nummerierte Liste, `> ` → Zitat, ` ``` ` → Codeblock, `---` → Trennlinie, `[ ] ` am Anfang eines Listenpunkts → Aufgabe, `**fett**`, `*kursiv*`, `~~durchgestrichen~~`, `` `Code` ``. Eingefügter Markdown-Text wird umgewandelt.
- Tastatur: Strg+B/I/E/K (fett, kursiv, Code, Link), Strg+Umschalt+X (durchgestrichen), Strg+Umschalt+7/8/9 (Listen), Strg+Alt+0…4 (Absatz, Überschriften), Strg+Z/Strg+Y. **Alt+F10** springt in die Werkzeugleiste (Pfeiltasten wählen, Escape zurück in den Text). **Tab** rückt in Listen ein und springt in Tabellen zur nächsten Zelle; **Escape, dann Tab** verlässt den Editor immer. **Enter** auf einem ausgewählten Makro oder Bild öffnet dessen Einstellungen.
- **Markdown** (rechts in der Werkzeugleiste) zeigt den Quelltext; ein zweiter Klick wechselt zurück.
- **Live-Bearbeitung:** Bearbeiten mehrere Personen dieselbe Seite, sehen alle die Änderungen und Cursor der anderen sofort. Über dem Text steht, wer mitbearbeitet; Kommen und Gehen wird für Screenreader angesagt. Rückgängig betrifft nur die eigenen Änderungen. Speichert jemand, bekommen die anderen einen Hinweis und speichern ohne Konflikt weiter. Jede Person hat eine eigene Kombination aus Farbe und Form (●, ■, ▲, ◆); dieselbe Markierung steht am Cursor und in der Liste der Mitbearbeitenden über dem Text. Live bearbeitet wird in der gerade aktiven Ansicht – visuell oder **Markdown-Quelltext** (auch dort mit den Cursorn der anderen). Wer mit *Markdown* umschaltet, schaltet für alle um; das wird angesagt, der Fokus bleibt bei jeder Person, wo er ist.

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
Im Editor öffnet der Knopf **Bild einfügen** die Medienbibliothek: Bild auswählen oder neu hochladen (Standard: Kategorie der Seite) und einen Alternativtext angeben, der das Bild für Menschen beschreibt, die es nicht sehen können. Andere Dateien werden als Link eingefügt. Das Makro **Bildergalerie** zeigt ausgewählte Bilder (`media="12,13,14"`) oder alle Bilder einer Kategorie als Raster; ein Klick öffnet das Bild groß, mit den Pfeiltasten blättern Sie weiter.

### Makros
Makros sind Bausteine, die beim Anzeigen der Seite ausgefüllt werden. Im visuellen Editor fügt der Knopf **Makro einfügen** (⚙) eines ein; Makros mit Inhalt (z. B. Hinweis-Box) umschließen markierte Absätze. Ein eingefügtes Makro erscheint als gestrichelter Kasten mit Name, Einstellungen sowie den Knöpfen *Makro bearbeiten* (✎) und *Makro entfernen* (×); Makros im Text (z. B. Status-Label, Textfarbe) als kleiner Baustein. Manche Makros haben einen eigenen Knopf in der Werkzeugleiste (◉ Status-Label, **A** Textfarbe und Schrift); der markierte Text wird dabei übernommen.

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
| Shell-Befehl | Ausgabe eines Befehls auf dem Server; läuft erst, wenn ein Superadmin genau diesen Befehl freigegeben hat |
| HTTP-Abruf | Ergebnis einer Web-Adresse (Text, HTML, JSON als Tabelle); nur freigegebene Adressen |

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
| Ende | schließt den Vorgang mit einem Ergebnis (erfolgreich, nicht erfolgreich, neutral) |

Bei Aufgaben und Freigaben kann eine **Erinnerung nach X Tagen** eingestellt werden. Zuständige erhalten bei neuen Aufgaben eine E-Mail (abschaltbar im Profil).

**Platzhalter** in E-Mails: `{{field.<id>}}` (Angaben aus dem Formular, z. B. `{{field.email}}`), `{{case.title}}`, `{{case.id}}`, `{{case.url}}`, `{{case.step}}`, `{{workflow.title}}`, `{{starter.name}}`, `{{starter.email}}`, `{{actor.name}}` und `{{actor.email}}` (wer den Schritt zuletzt bearbeitet hat), `{{last.action}}`, `{{last.note}}` (Aktion und Notiz dieser Person), `{{site.name}}`, `{{date}}`. Im Editor fügt ein Klick auf einen Platzhalter ihn in das zuletzt benutzte Feld ein. Als Empfänger sind außerdem `starter`, `actor`, `user:name`, `group:name` und `role:name` möglich.

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

### Blog, FAQ, Veranstaltungskalender und Team

Beim Anlegen eines Elements (*Neues Element*) wählen Sie den Typ:

- **Blog**: zeigt die Seiten einer Kategorie (optional mit Unterkategorien) als Beiträge, die neuesten zuerst. Es gibt Anrisstext, Autor und Datum sowie die Layouts Liste, Karten und Kompakt, dazu Blättern, Schlagwort-Filter (Klick auf einen Tag) und eine **Suche nur im Blog**. Die Beiträge sind normale Seiten: **Neuer Beitrag** öffnet den Editor in der Blog-Kategorie, **Neue Unterkategorie** legt z. B. „Veranstaltungen“ an. Im Editor des Blogs wählen Sie die Kategorie aus oder legen sie mit **Neue Kategorie** an.
- **Gemeinsame Notizen**: eine Seite, auf der alle mit Bearbeitungsrecht direkt in der Ansicht schreiben – gleichzeitig, mit den Cursorn der anderen (Live-Bearbeitung). Gespeichert wird automatisch wenige Sekunden nach einer Änderung (höchstens alle 30 Sekunden eine neue Version, im Verlauf als „Notizen automatisch gespeichert“). Leser:innen sehen die Notizen als normale Seite. Ideal für Besprechungsprotokolle und gemeinsame Aufgabenlisten.
- **FAQ**: Unterkategorien werden zu aufklappbaren Abschnitten, Seiten zu aufklappbaren Fragen (Titel = Frage, Inhalt = Antwort). Das Suchfeld filtert sofort, *Alle aufklappen/zuklappen* hilft beim Lesen. **Neue Frage** legt Frage und Antwort direkt im Dialog an (Markdown), **Neuer Abschnitt** eine Unterkategorie. Jede Frage hat einen eigenen Link (`#q-123`).
- **Veranstaltungskalender**:
  - Ansichten Monat, Woche und Liste, Kategorien mit Farben, Suche.
  - Wiederkehrende Termine: täglich, wöchentlich an bestimmten Tagen, monatlich am Tag X oder z. B. „am letzten Freitag“, jährlich; mit Ende nach Datum oder Anzahl. Einzelne Termine einer Serie lassen sich absagen oder mit **Nur diesen Termin bearbeiten** ändern (Titel, Ort, Beschreibung, Datum und Uhrzeit). Geänderte Termine sind gekennzeichnet, Anmeldungen bleiben erhalten, und Angemeldete erhalten bei neuer Zeit oder neuem Ort eine E-Mail. **Änderung zurücksetzen** stellt den Termin wie in der Serie wieder her; **Serie bearbeiten** ändert alle Termine.
  - **Anmeldung** mit Platzzahl, Warteliste und Anmeldeschluss. Bestätigungen kommen per E-Mail mit einer **Kalendereinladung**: Outlook, Google oder Apple Kalender tragen den Termin direkt ein, aktualisieren ihn bei Änderungen und entfernen ihn bei Abmeldung oder Absage (je Kalender abschaltbar). Wird ein Platz frei, rückt automatisch die erste Person der Warteliste nach und wird informiert.
  - **Erinnerungen per E-Mail** (z. B. 1 Tag vorher) an Angemeldete und alle, die „Erinnern“ gewählt haben.
  - **In meinen Kalender (.ics)** für einen Termin und **Abonnieren** des ganzen Kalenders in Outlook, Google oder Apple Kalender (persönlicher Link). Organisatoren sehen die Teilnehmenden und können sie als CSV exportieren.
  - Das Makro **Nächste Termine** zeigt kommende Termine auf jeder Seite.
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
- **Eigenschaften-Datenbank** (Elementtyp): alle Elemente einer oder mehrerer Quellen als Tabelle, Karten, Board, Kalender, Zeitleiste oder Diagramm – suchen, filtern, gruppieren, Werte einer Zeile direkt ändern, *Neuer Eintrag*, CSV-Download.
- **Makro „Eigenschaften-Bericht“:** im Makro-Dialog Quellen (Kategorien mit/ohne Unterkategorien, Schlagwörter, Elementtypen, einzelne Elemente), Filter mit passenden Bedingungen je Typ (z. B. *Fälligkeit höchstens today+30d*, *Verantwortlich bin ich*), Spalten, Sortierung und Darstellung wählen: **Tabelle** (mit Gruppen, Zwischensummen und Summen), **Liste**, **Karten**, **Board** (Karten per Auswahlfeld oder Ziehen in eine andere Spalte verschieben), **Kalender**, **Zeitleiste**, **Diagramm** (Säulen, Balken, gestapelt, Linie, Fläche, Kreis, Ring, Heatmap), **Kennzahlen**, **Pivot-Tabelle**, **Schlagwort-Wolke**, **Vollständigkeit** (welche Werte fehlen), **Überfällig und bald fällig** und **Orte**. Lesende können eigene Filter setzen und als CSV herunterladen. Diagramme haben eine Textzusammenfassung und die Daten als Tabelle.
- Alles zeigt nur, was Sie sehen dürfen: Elemente ohne Leserecht und eingeschränkt sichtbare Eigenschaften fehlen auch in Filtern, Summen und Diagrammen.

### Adressen und „Seite nicht gefunden“
Jede Seite und Kategorie hat **je Sprache eine lesbare Adresse** aus ihrem Titel, z. B. `/wiki/unser-team/ueber-mich` und `/wiki/our-team/about-me`; ohne Übersetzung gilt die Adresse der Originalsprache. Alle Sprachadressen funktionieren, die Adresszeile zeigt die Adresse in Ihrer Sprache. Mit **Nur diese Kurz-URL** (bei der Kurz-URL in den Eigenschaften) ist eine Seite nur über ihre Kurz-URL erreichbar. Wird eine Seite umbenannt oder verschoben oder eine Kurz-URL geändert, führen die **alten Adressen weiterhin zum Ziel** (dauerhafte Weiterleitung). Führt eine Adresse ins Leere, zeigt das Wiki, ob die Seite gelöscht wurde (mit Link zum Papierkorb für alle, die wiederherstellen dürfen), und schlägt passende Seiten vor („Meinten Sie?“) – nur solche, die Sie sehen dürfen.

### Kurz-URLs

Elemente und Kategorien können eine **Kurz-URL** bekommen: Seiten über *Eigenschaften* (dort auch das **Vorschaubild**, das beim Teilen eines Links in Teams, Slack oder Outlook erscheint – ohne Angabe das erste Bild der Seite), Kategorien über *Kategorie bearbeiten* bzw. die Einstellungen. Mit `kalender` ist das Element dann unter `https://ihr-wiki/kalender` erreichbar. Unterebenen wie `team/hr` sind möglich. Jede Kurz-URL gibt es nur einmal; Wörter, die das Wiki selbst verwendet (`admin`, `login`, `search`, …), sind gesperrt. Menüs und Übersichten verlinken automatisch die Kurz-URL.

### Startseite und Kategorieübersicht

Unter *Administration → Einstellungen → Startseite* wählen Administratoren, was unter `/` erscheint: die Übersicht der obersten Kategorien, eine Kategorie oder ein Element. Neue Installationen zeigen die Willkommensseite. Für eine Übersicht von Bereichen eignet sich der Elementtyp **Kategorieübersicht**: eine Kategorie oder die oberste Ebene, mit 1–5 Ebenen Unterkategorien, als Karten oder Liste, auf Wunsch mit Beschreibungen und den Seiten jeder Kategorie.

### Karussells
Karussells zeigen an einer Position der Vorlage (z. B. über dem Inhalt) Neuigkeiten, eigene Folien und Einträge externer Feeds. Beim **Veröffentlichen mit Optionen** wählen Sie mit dem Recht *Neuigkeiten in Karussells veröffentlichen* „Als Neuigkeit in …“ und optional ein Enddatum; Neuigkeiten sehen nur Personen, die die Seite sehen dürfen. Mit *‹* und *›* oder den Punkten wechseln Sie die Folie; läuft das Karussell automatisch, hält *Pause* es an (es hält auch an, solange Maus oder Fokus darin sind, und läuft gar nicht, wenn Ihr System reduzierte Bewegung wünscht).

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

Die **Glocke** neben Ihrem Namen zeigt Benachrichtigungen im Wiki, die Zahl ungelesener steht daneben. Ein Klick auf eine Benachrichtigung öffnet das Ziel und markiert sie als gelesen; *Alle als gelesen markieren* leert die Liste der ungelesenen.

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

Eine ganze Kategorie setzen Sie unter *Kategorie-Einstellungen → Verlauf & Zurücksetzen* auf einen **Zeitpunkt** zurück, wahlweise mit allen Unterkategorien. Titel, Texte und Seiten bekommen den damaligen Stand, später angelegte Seiten kommen in den Papierkorb. Auch das lässt sich wieder rückgängig machen.

## Papierkorb

Gelöschte Kategorien (mit allem Inhalt) und Seiten landen im **Papierkorb** (Benutzermenü). Von dort stellen Sie sie wieder her. Endgültig gelöscht wird nur durch Administratoren oder automatisch nach der eingestellten Aufbewahrungszeit.

## Suche und Tags

- Die **Suche** findet Wörter und Wortanfänge in Titeln, Tags und Inhalten aller Sprachen. Treffer sind hervorgehoben.
- **Tags** vergeben Sie bei Kategorien (Einstellungen) und Seiten (*Tags*). Ein Klick auf einen Tag zeigt alle Inhalte mit diesem Tag. Auf der Suchseite gibt es eine Liste beliebter Tags.

## Berechtigungen einer Kategorie

Mit dem Recht *Verwalten* legen Sie unter *Kategorie-Einstellungen → Berechtigungen* fest, wer was darf: Benutzer, Gruppe, Rolle, alle Angemeldeten oder Gäste. Wählen Sie eine Vorlage (**Leser**, **Kategorie-Editor**, **Kategorie-Manager**) oder einzelne Rechte, auch die Rechte von Plugins (z. B. *Workflows anlegen*). Optional gilt eine Regel auch für alle Unterkategorien. Geerbte Regeln werden mit ihrer Herkunft angezeigt. Details: [docs/admin/security.md](../admin/security.md#berechtigungen).

## Anmelden

Je nach Einrichtung melden Sie sich mit Benutzername und Passwort an (lokales Konto oder LDAP/Active Directory) oder mit einem Knopf **Anmelden mit …** über den zentralen Login Ihrer Organisation (OpenID Connect oder Shibboleth/SAML). Haben Sie weitere Faktoren eingerichtet (oder verlangt die Administration sie), fragt das Formular danach Schritt für Schritt nach ihnen, z. B. nach dem Code aus Ihrer App; fehlt Ihnen ein verlangter Faktor, richten Sie ihn gleich dort ein. Mit einem **Passkey** melden Sie sich, wo erlaubt, ganz ohne Passwort an.

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
