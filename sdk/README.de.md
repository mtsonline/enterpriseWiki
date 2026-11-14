# Enterprise Wiki – Plugin-SDK

> English version: [README.md](README.md)

**Stand: im Aufbau.** Dieses Modul enthält noch keine Schnittstelle.

Das SDK wird die stabile Schnittstelle für Plugins, die als eigener, abgeschotteter Prozess neben Enterprise Wiki
laufen: ein versionierter Vertrag (`ewiki.plugin.v1`, Protobuf über gRPC auf einem Unix-Socket), signierte Pakete,
Rechte, denen der Admin bei der Installation zustimmt, und eine lange Stabilitätszusage je Hauptversion.

Bis es so weit ist, sind Plugins **Core-Plugins**: Sie werden mit dem Wiki kompiliert und nutzen `internal/coreplugin`.
Diese Schnittstelle ändert sich mit dem Kern (siehe [docs/de/developer/plugins.md](../docs/de/developer/plugins.md)).

## Lizenz

Anders als der Rest des Repositorys steht dieses Verzeichnis unter der
[Apache License, Version 2.0](LICENSE), damit Plugins anderer Anbieter unter eigener Lizenz entstehen können.
