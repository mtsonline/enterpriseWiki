# Mitwirken

Danke für Ihr Interesse! Beiträge sind willkommen – Fehlerberichte, Übersetzungen, Dokumentation, Code und Plugins.

1. **Beitragsvereinbarung**: Pull Requests können nur übernommen werden, wenn Sie der [CLA](CLA.md) zustimmen (Kästchen
   in der Pull-Request-Vorlage). Hintergrund: Enterprise Wiki wird unter der Business Source License und unter
   kommerziellen Lizenzen vertrieben (siehe [LICENSING.md](LICENSING.md)).
2. **Abhängigkeiten**: Neue Bibliotheken nur mit erlaubten Lizenzen (MIT, BSD, Apache-2.0, ISC …, siehe LICENSING.md).
   Die CI prüft das automatisch.
3. **Qualität**: `./scripts/build.sh`, Go-Tests (`go test ./...`, mit `EWIKI_TEST_DATABASE_URL` auch die
   API-Integrationstests) und die Browser-Tests mit Barrierefreiheitsprüfung müssen grün sein. Jede Funktion gehört in
   API, CLI und Oberfläche.
4. **Plugins** für den offiziellen Katalog: siehe [docs/developer/plugins.md](docs/developer/plugins.md); auch hier gilt die CLA.
