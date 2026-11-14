# Enterprise Wiki – Plugin SDK

> Deutsche Fassung: [README.de.md](README.de.md)

**Status: under construction.** This module does not contain an API yet.

The SDK will be the stable interface for plugins that run as their own, isolated process next to Enterprise Wiki:
a versioned contract (`ewiki.plugin.v1`, Protobuf over gRPC on a Unix socket), signed packages, permissions that an
administrator approves at installation, and a long stability promise for each major version.

Until it is ready, plugins are **core plugins**: they are compiled into the wiki and use `internal/coreplugin`, which
changes together with the core (see [docs/en/developer/plugins.md](../docs/en/developer/plugins.md)).

## License

Unlike the rest of the repository, this directory is licensed under the
[Apache License, Version 2.0](LICENSE), so that plugins of other vendors can use their own license.
