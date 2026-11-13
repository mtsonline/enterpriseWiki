# Contributing

Thank you for your interest! Contributions are welcome – bug reports, translations, documentation, code and plugins.

The binding rules for all contributions are in [CLAUDE.en.md](CLAUDE.en.md) (German original: [CLAUDE.md](CLAUDE.md); for humans and AI agents alike).

1. **Contributor agreement**: pull requests can only be accepted if you agree to the [CLA](CLA.md) (checkbox
   in the pull request template). Background: Enterprise Wiki is distributed under the Business Source License and under
   commercial licenses (see [LICENSING.md](LICENSING.md)).
2. **Dependencies**: new libraries only with allowed licenses (MIT, BSD, Apache-2.0, ISC …, see LICENSING.md).
   The CI checks this automatically.
3. **Quality**: `./scripts/build.sh`, Go tests (`go test ./...`, with `EWIKI_TEST_DATABASE_URL` also the
   API integration tests) and the browser tests with accessibility check must be green. Every feature belongs in
   API, CLI and interface.
4. **Plugins** for the official catalogue: see [docs/en/developer/plugins.md](docs/en/developer/plugins.md); the CLA applies here too.
