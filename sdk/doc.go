// Package sdk will be the stable interface for plugins that run as their own
// process next to Enterprise Wiki.
//
// It is under construction and contains no API yet: the contract
// (ewiki.plugin.v1, Protobuf over gRPC on a Unix socket) is designed next.
// Until then plugins are core plugins built on internal/coreplugin, which
// has no stability promise.
//
// This module is licensed under the Apache License, Version 2.0, so that
// plugins of other vendors can use their own license.
package sdk
