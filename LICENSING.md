# Licensing of Enterprise Wiki

> Translation for information. If this text and the German version [LICENSING.de.md](LICENSING.de.md) differ, the German version applies.

> Summary: Enterprise Wiki is licensed under the **Business Source License 1.1** (BSL, see [LICENSE](LICENSE)).
> **Private individuals** use it for private purposes **free of charge**. **Organisations** – companies,
> public bodies, schools and universities, associations – need a **license**.
> Every version additionally becomes open source (AGPL-3.0) four years after its release.

Until the first version that is not marked as a development version, the license type may still change. A change only applies to versions from the date it takes effect; versions already released keep their license.

This page explains the license in plain words. Legally binding is the text in [LICENSE](LICENSE)
or the respective license agreement.

## Who may do what?

| Use | Without license | With license |
|---|---|---|
| Read, change, pass on the source code | ✔ (under the same license) | ✔ |
| Testing, development, evaluation (no productive use) | ✔ for everyone | ✔ |
| Productive use as a **private individual** for private purposes (family wiki, private website) | ✔ free of charge | – |
| Productive use by or for **companies**, self-employed people, professionally | ✘ | ✔ license "Business" |
| **Public bodies** (authorities, municipalities, offices) | ✘ | ✔ discounted |
| **Education and research** (schools, universities, research institutions) | ✘ | ✔ discounted (EDU) |
| **Associations**, foundations, non-profit organisations | ✘ | ✔ discounted |
| Offering it as a hosted service for third parties | ✘ | only with a separate agreement |

Four years after a version is released, the AGPL-3.0-or-later additionally applies to **this** version: from then on
anyone may use it under the rules of the AGPL. Newer versions stay under the BSL until their own change date.

## License keys

Organisations receive a **license key** (starts with `EWL1.`). It contains licensee, license type,
number of users and term and is digitally signed (Ed25519). The check happens **offline** – the wiki
does not connect to the outside for it.

- Installing: *Administration → License* or `ewiki license install KEY` (stored in `config.yaml` under `license.key`).
- Showing: `ewiki license show`, API `GET /api/v1/admin/license`.
- All **active** (not disabled) user accounts are counted.

The key **does not lock any features**. If it has expired or more users are active than licensed, the
administration points this out clearly. The obligation to have a license results from the license itself, not from the key.

## Contained third-party components

All components built into the delivered program are licensed under **permissive** licenses
(MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC) or, for fonts, under the **SIL Open Font License 1.1**.
These licenses allow use in a product under the BSL, in commercial licenses and later under the
AGPL. There is therefore **no license conflict**. To be fulfilled:

- **Ship copyright notices and license texts**: when building, `tools/notices` creates the file `licenses.txt`
  with all license texts (including the NOTICE files of Apache-2.0 components). It is contained in the program,
  available under `/licenses.txt`, under *Administration → License* and with `ewiki license notices`.
- **Fonts** (Inter, Source Sans 3; OFL 1.1): may be bundled with software, but not sold on their own; the
  license text is contained in `licenses.txt`.

Only for **building and testing** (not in the delivered program): TypeScript (Apache-2.0), esbuild (MIT), Playwright
(Apache-2.0), axe-core (MPL-2.0), gldap (MIT, only tests). These licenses place no requirements on the product.

**Docker image**: it additionally contains programs from Alpine Linux (among others BusyBox, musl) and `poppler-utils`
(`pdftotext`, GPL). These run as **separate programs** next to Enterprise Wiki (not compiled in); this is
a permitted aggregation and does not affect the license of Enterprise Wiki. Alpine Linux provides the sources of these packages
(<https://pkgs.alpinelinux.org>).

### Rules for new dependencies

On every build the CI checks with `go run ./tools/notices -check` that all built-in Go modules and all npm packages
of the frontend (`web/package-lock.json`; except pure test tools such as axe-core and Playwright) have an allowed license:

- **Allowed**: MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC, Zlib, Unlicense/CC0; fonts under OFL-1.1.
- **Only after review**: MPL-2.0 (changes to these files must be disclosed).
- **Not allowed** in the program: GPL, LGPL, AGPL (would make the BSL licensing impossible), SSPL, BSL/"source
  available" licenses of third parties, licenses with usage restrictions (e.g. "non-commercial"), code without a license.

## Contributing and plugins

So that Enterprise Wiki can continue to be distributed under the BSL **and** under commercial licenses, the
licensor needs the necessary rights to every contribution. Therefore:

- **Contributions to the repository** (pull requests, patches, translations, documentation) are only accepted if the
  contributing person has agreed to the [contributor license agreement (CLA)](CLA.md). They keep their copyright, but grant
  the licensor a comprehensive, unlimited right of use including sublicensing and relicensing.
- **Plugins in the official plugin catalogue** or plugins delivered with Enterprise Wiki also require
  agreement to the CLA (section "Plugins" of the CLA).
- **Own plugins for your own use** (e.g. an internal plugin of an organisation) do not need a CLA. They
  may be under any license compatible with the BSL. A program that contains Enterprise Wiki together with
  the plugin is still subject as a whole to the BSL or the license agreement.

## License types and prices

| License type | For | Key edition |
|---|---|---|
| Business | companies, self-employed people, professional use | `business` |
| Public body | authorities, municipalities, public companies | `public` |
| Education and research | schools, universities, research institutions | `edu` |
| Association / non-profit | associations, foundations, NGOs | `nonprofit` |
| Trial license | time-limited evaluation with productive data | `trial` |

Licensor, prices and enquiries:

**thinking systems**, owner Martin Thomas Schrott
Höhenweg 114, 9073 Klagenfurt am Wörthersee, Austria

## Frequently asked questions

**I use the wiki privately and would like to organise my association with it.** Use for the association is no longer private
use; the discounted association license exists for this.

**We would like to test Enterprise Wiki.** Testing and evaluating is allowed without a license as long as there is no productive use.
Trial licenses exist for a pilot operation with real users.

**May we adapt the code?** Yes. Adapted versions are also subject to the BSL; productive use in
an organisation still needs a license.

**What happens after four years?** The version concerned may then additionally be used under the AGPL-3.0.
Updates, security fixes and support for current versions continue to be available with a license.
