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

### Neue Unterkategorie
Die aktuell geöffnete Kategorie ist automatisch als übergeordnete Kategorie ausgewählt. Titel und Beschreibung können Sie für jede Sprache eingeben.

### Seiten bearbeiten
- **Formatierter Text:** Die Werkzeugleiste bietet Absatz, Überschriften, fett, kursiv, unterstrichen, durchgestrichen, Listen, Zitat, Code, Link, Tabelle und Trennlinie. Tastenkürzel: Strg+B / Strg+I / Strg+U, Strg+Z rückgängig.
- **Markdown:** Format auf *Markdown* umstellen. Mit *Vorschau* sehen Sie das Ergebnis.
- Geben Sie optional eine **Änderungsnotiz** ein. Sie erscheint im Verlauf.
- Hat jemand die Seite gleichzeitig gespeichert, werden Sie gewarnt, statt die fremde Änderung zu überschreiben. Laden Sie dann neu und übernehmen Sie Ihre Änderungen.

### Dateien
Wählen Sie bei *Neue Seite* den Elementtyp **Datei**, laden Sie die Datei hoch und beschreiben Sie sie. PDFs, Bilder und Videos werden direkt angezeigt, andere Dateien stehen zum Herunterladen bereit. Eine neue Datei hochzuladen und zu speichern erzeugt eine neue Version. Frühere Dateien bleiben im Verlauf erhalten. Der Text von PDFs ist über die Suche auffindbar.

### Bilder in Seiten
Im Editor fügt der Knopf **Bild einfügen** ein Bild ein. Geben Sie dabei einen Alternativtext an, der das Bild für Menschen beschreibt, die es nicht sehen können.

### Makros
Makros sind Bausteine, die beim Anzeigen der Seite ausgefüllt werden. Im Editor fügt der Knopf **Makro** (⚙) eines ein; steht der Cursor in einem Makro, bearbeitet derselbe Knopf dessen Einstellungen. Im Markdown-Modus gibt es dafür *Makro einfügen* über dem Textfeld.

| Makro | Wirkung |
|---|---|
| Inhaltsverzeichnis | listet die Überschriften der Seite mit Sprungmarken |
| Unterseiten | Unterkategorien und Seiten dieser (oder einer anderen) Kategorie |
| Zuletzt geändert | die zuletzt geänderten Seiten |
| Hinweis-Box | Info, Notiz, Tipp, Warnung oder Erfolg, mit Titel |
| Aufklappbereich | Inhalt, der erst nach einem Klick erscheint |
| Status-Label | farbiges Label wie ERLEDIGT |
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

### Blog, FAQ, Veranstaltungskalender und Team

Beim Anlegen eines Elements (*Neues Element*) wählen Sie den Typ:

- **Blog**: zeigt die Seiten einer Kategorie (optional mit Unterkategorien) als Beiträge, die neuesten zuerst. Es gibt Anrisstext, Autor und Datum sowie die Layouts Liste, Karten und Kompakt, dazu Blättern, Schlagwort-Filter (Klick auf einen Tag) und eine **Suche nur im Blog**. Die Beiträge sind normale Seiten: **Neuer Beitrag** öffnet den Editor in der Blog-Kategorie, **Neue Unterkategorie** legt z. B. „Veranstaltungen“ an. Im Editor des Blogs wählen Sie die Kategorie aus oder legen sie mit **Neue Kategorie** an.
- **FAQ**: Unterkategorien werden zu aufklappbaren Abschnitten, Seiten zu aufklappbaren Fragen (Titel = Frage, Inhalt = Antwort). Das Suchfeld filtert sofort, *Alle aufklappen/zuklappen* hilft beim Lesen. **Neue Frage** legt Frage und Antwort direkt im Dialog an (Markdown), **Neuer Abschnitt** eine Unterkategorie. Jede Frage hat einen eigenen Link (`#q-123`).
- **Veranstaltungskalender**:
  - Ansichten Monat, Woche und Liste, Kategorien mit Farben, Suche.
  - Wiederkehrende Termine: täglich, wöchentlich an bestimmten Tagen, monatlich am Tag X oder z. B. „am letzten Freitag“, jährlich; mit Ende nach Datum oder Anzahl. Einzelne Termine einer Serie lassen sich absagen.
  - **Anmeldung** mit Platzzahl, Warteliste und Anmeldeschluss. Bestätigungen kommen per E-Mail; wird ein Platz frei, rückt automatisch die erste Person der Warteliste nach und wird informiert.
  - **Erinnerungen per E-Mail** (z. B. 1 Tag vorher) an Angemeldete und alle, die „Erinnern“ gewählt haben.
  - **In meinen Kalender (.ics)** für einen Termin und **Abonnieren** des ganzen Kalenders in Outlook, Google oder Apple Kalender (persönlicher Link). Organisatoren sehen die Teilnehmenden und können sie als CSV exportieren.
  - Das Makro **Nächste Termine** zeigt kommende Termine auf jeder Seite.
- **Team**: Name und Beschreibung (Markdown/HTML), eine oder mehrere Gruppen, deren Mitglieder angezeigt werden, und die Auswahl der Angaben: Foto, Funktion, Abteilung, E-Mail, Telefon, Mobil, Büro, „Über mich“, Gruppen oder Attribute aus LDAP/OIDC/SAML. Darstellung als Karten, Liste oder Tabelle, auf Wunsch nach Gruppen gegliedert, mit Filter und CSV-Export. Die Angaben pflegt jede Person unter *Mein Profil → Visitenkarte*, Administratoren unter *Administration → Benutzer*.

### Menüs

Unter *Administration → Menüs* (Administratoren) bestehen Menüs aus Einträgen mit einem Ziel:

- eine **Kategorie**,
- ein **Element** jeden Typs (Seite, Blog, FAQ, Kalender …),
- ein **freier Link**,
- ein **anderes Menü** als aufklappbares Untermenü (für mehrstufige Menüs),
- eine **Überschrift**, die nur die eingerückten Einträge darunter gruppiert.

Kategorien und Elemente wählen Sie aus den vorhandenen aus oder legen sie über **Neue Kategorie** bzw. **Neues Element** direkt an. Bleibt die Beschriftung leer, erscheint der Titel des Ziels in der jeweiligen Sprache. Einträge, die jemand nicht sehen darf, werden ihm nicht angezeigt.

Menüs ohne Platz („nur als Untermenü“) können in mehreren Menüs eingehängt werden. In der Kopfzeile öffnen sich Untermenüs als Ausklapp-Menüs (per Maus, Tastatur, Esc schließt), in der Seitenleiste als aufklappbarer Baum, im Fußbereich vollständig.

## Beobachten

Mit **Beobachten** (auf jeder Seite und Kategorie, für angemeldete Personen) erhalten Sie eine E-Mail, wenn sich etwas ändert, bei Kategorien auf Wunsch auch für alle Unterkategorien. Sie wählen, ob die E-Mail **nur den Link** oder **Link und neuen Inhalt** enthält. Mehrere Änderungen kurz hintereinander kommen gesammelt in einer E-Mail. Eigene Änderungen werden nicht gemeldet, und es werden nur Inhalte gemeldet, die Sie sehen dürfen. Ihre Beobachtungen verwalten Sie unter *Mein Profil → Beobachtungen*.

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

Mit dem Recht *Berechtigungen verwalten* legen Sie unter *Kategorie-Einstellungen → Berechtigungen* fest, wer was darf: Benutzer, Gruppe, Rolle, alle Angemeldeten oder Gäste, mit den Rechten Ansehen, Bearbeiten, Erstellen, Löschen/Verschieben und Verwalten. Optional gilt eine Regel auch für alle Unterkategorien. Geerbte Regeln werden mit ihrer Herkunft angezeigt. Details: [docs/admin/security.md](../admin/security.md#berechtigungen).

## Anmelden

Je nach Einrichtung melden Sie sich mit Benutzername und Passwort an (lokales Konto oder LDAP/Active Directory) oder mit einem Knopf **Anmelden mit …** über den zentralen Login Ihrer Organisation (OpenID Connect oder Shibboleth/SAML). Ist für Ihr Konto die Zwei-Faktor-Authentifizierung aktiv, fragt das Formular anschließend nach dem Code aus Ihrer App.

## Mein Profil

- Anzeigename, E-Mail und bevorzugte Sprache,
- Passwort ändern und **Zwei-Faktor-Authentifizierung** einrichten (lokale Konten),
- **API-Keys** für Skripte: Rechte, Kategorie und Ablaufdatum wählen. Der Schlüssel wird nur einmal angezeigt,
- **SSH-Schlüssel** für die passwortlose Anmeldung an der API,
- **Visitenkarte** (Foto, Funktion, Abteilung, Telefon, Büro, „Über mich“) für Team-Seiten,
- **Beobachtungen** und **Feed- und Kalender-Links**.

## Barrierefreiheit

Die Oberfläche ist vollständig per Tastatur bedienbar (Tab, Umschalt+Tab, Enter, Leertaste, Esc schließt Dialoge und Menüs). Mit **Zum Inhalt springen** (erste Tab-Taste) überspringen Sie die Navigation. Alle Bedienelemente sind für Screenreader beschriftet, Statusmeldungen werden angesagt, und nach einem Seitenwechsel springt der Fokus auf die Überschrift.
