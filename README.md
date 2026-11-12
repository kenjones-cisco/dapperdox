# DapperDox&nbsp;&nbsp;&nbsp;[![Build Status](https://travis-ci.org/DapperDox/dapperdox.svg?branch=master)](https://travis-ci.org/DapperDox/dapperdox) [![Download](https://img.shields.io/badge/release-v1.1.1-green.svg) ](https://github.com/DapperDox/dapperdox/releases/tag/v1.1.1)

### Beautiful, integrated, OpenAPI documentation.

> Themed documentation generator, server and API explorer for OpenAPI (Swagger) Specifications. Helps you build integrated, browsable reference documentation and guides. For example, the [Companies House Developer Hub](https://developer.companieshouse.gov.uk/api/docs/) built with the alpha version.

![DapperDox logo](/docs/logo-dapperdox-github.png "DapperDox")

## Features

* Author full documentation in GitHub Flavoured Markdown.
* Document multiple API specifications as a suite of cross-referenced products.
* Seamlessly overlay content onto the automatically generated reference documentation.
* Integrate the built-in API explorer with your APIs and authentication model.
* Proxy your developer platform, allowing full integration of API key management.
* Choose from multiple themes, or create your own.

## Documentation and usage

Detailed usage instructions are available on the [DapperDox website http://dapperdox.io](http://dapperdox.io).

## Quickstart

We provide the lastest release builds for the most common operating systems and architectures, which you
can download from [releases](https://github.com/DapperDox/dapperdox/releases),
or you can build from source by cloning this repo..
See [downloads](http://dapperdox.io/download/downloads) for further information.

### Build from source

Use Go **1.26.11** (the MCMP Go 1.26 line). The development image pins
golangci-lint **v2.12.2**, matching the MCMP green image.

First build DapperDox:
```bash
go mod download
go build .
```

For formatting and static analysis, use `make check`, or install the pinned
golangci-lint version and Mike Farah's `yq` v4 and run:

```bash
bash scripts/check.sh --fix
bash scripts/check.sh --check
go test -race ./...
```

`--check` (also accepted as `--ci`) does not modify source files and fails if
formatting or lint changes are required. The default and `--fix` apply fixes.
Unlike the shared MCMP script, no submodule-cleaning script is sourced: this
repository has no `cicd-common` submodule.

The script merges the image's `/usr/local/etc/golangci.yml` with
`.golangci.yml`. Outside the image it uses the tracked snapshot
[`scripts/golangci-base.yml`](scripts/golangci-base.yml), from
[MCMP images revision `4cc5b99`](https://github.com/cisco-it-mcmp/images/blob/4cc5b997d84887ccb24cc123b306b824c10d7074/golang-dev/green/golangci.yml).
Set `GOLANGCI_BASE_CONFIG` to validate against another baseline explicitly.
Repository overrides only add local import aliases and the main-program output
exception; the former complexity and unused-assignment suppressions are removed.

Kubernetes modules (`api`, `apimachinery`, and `client-go`) are kept together at
**v0.33.13** for Kubernetes **v1.33.x**, rather than following newer Kubernetes
minor releases. Other dependencies use their latest compatible releases.
`kube-openapi` and `gnostic-models` remain on the compatible Kubernetes v0.33
dependency versions: newer `kube-openapi` requires Go 1.27, while
`gnostic-models` v0.7 changes YAML types incompatibly with this version.

Application logging uses standard-library `log/slog` with structured text on
stderr, package fields and fixed-width timestamps. Supported `--log-level`
values are `trace`, `debug`, `info`, `warn`/`warning`, and `error`; invalid values
fail startup. Standard-library diagnostics remain at debug level.

The built-in API proxy uses `ReverseProxy.Rewrite`: target host, path, and query
are preserved, while incoming forwarding headers are replaced with values
derived from the actual client request.

### Compatibility tests

Run the browser fixtures with Docker:

```bash
make test-browser
```

The development image installs Node and pinned Playwright Chromium, Firefox
and WebKit. It also exercises mobile Safari emulation; no host npm or browser
installation is needed. The suite starts a local documentation/API fixture
server and exercises the actual self-hosted scripts, styles, fonts and templates.
It covers authentication hooks, header/query API keys, Basic/Bearer precedence,
JSON bodies, server-received multipart bytes, binary downloads (including NUL
and non-UTF-8 bytes), response highlighting, HTTP errors, and navigation.
Offline page checks reject external asset requests and JavaScript errors.

Set `BROWSER_TEST_THEME=sectionbar` to test the other built-in theme. For consumer
integration, mount the consuming repository's assets and set
`BROWSER_TEST_ASSETS` to their container path and `BROWSER_TEST_THEME=cisco`.
`BROWSER_SCREENSHOTS` saves desktop/mobile evidence to a mounted directory.
`BROWSER_TEST_CONTRACTS` can point at captured guide DOM contracts to compare
heading targets, links, images and code text before and after a migration.
If pinned consumer content has pre-existing missing assets, list their exact
URL paths in `BROWSER_TEST_EXPECTED_404` (newline-delimited). The suite requires
exactly those 404s; without that setting every failed asset request fails.
JavaScript and webfont MIME types are set by the production static handler,
independently of the image's MIME database.

Go HTTP fixtures in [handlers/compatibility_test.go](handlers/compatibility_test.go)
protect public URLs, redirects, heading anchors, Markdown guides, section-overlay
precedence, explorer markup, and specification downloads.

### Browser assets

The supported browser floor is the current and previous stable major releases
of Chrome, Edge, Firefox and Safari, including mobile Safari; Internet Explorer
is not supported. Playwright engine tests and mobile emulation are not a
substitute for release qualification on branded browsers and real iOS devices.

All built-in scripts, styles, icons and Open Sans fonts are self-hosted.
`package-lock.json` pins jQuery 4.0.0, Bootstrap 5.3.9, Highlight.js 11.12.0,
Font Awesome Free 7.3.1 and Open Sans 5.3.0. No production Node process or CDN
is needed. The image packages defaults at `/opt/dapperdox/assets` and sets
`DEFAULT_ASSETS_DIR` accordingly. Set `ASSETS_DIR` to the consuming repository's
assets directory so custom guides, themes and authentication hooks override the
packaged defaults.

Generate and verify committed distributions and license notices in Docker:

```bash
make browser-assets
make check-browser-assets
```

The source bundle is `scripts/browser.js`; its generator copies the locked
styles, font files and upstream licenses to `assets/static/browser`.
Bootstrap 5 navigation uses `data-bs-toggle`/`data-bs-target` and the `show`
collapse class. Custom layouts must use `/browser/browser.js`,
`/browser/bootstrap.css`, `/browser/highlight.css` and the built-in fonts
fragment instead of legacy/CDN scripts. Keep the existing explorer IDs and
`apiExplorer.setBeforeSendCallback` contract. The removed wiggle plugin is
replaced by focusing invalid fields; native object-URL downloads replace
FileSaver. Explorer responses are read as bytes, preserving non-UTF-8 downloads.
Displayed text honors the response's declared charset (UTF-8 when absent);
unsupported encodings appear in the existing explorer error display.

This migration changes layout spacing and responsive behavior, not guide URLs,
server-side Markdown or authentication APIs. Consumer overrides must adopt it
with the matching image. Roll back the consuming image and assets together.
The browser migration can be reverted independently of the Markdown engine.

### Markdown rendering

Markdown uses Goldmark **v1.8.7** with GFM extensions and Chroma **v2.27.0**
for server-side `Go`, `Go-unformatted`, and `diff` highlighting. Other fenced
languages remain escaped, unhighlighted code; this change does not add languages
or alter browser-side highlighting. Bluemonday's existing sanitization policy
still applies to all generated output, including embedded author HTML.

Existing named heading anchors (including duplicate names), code wrappers,
theme token classes, custom `gfm.map` processing, and supported task-list
checkboxes are retained. GFM formatting in headings, including strikethrough,
is preserved in the visible heading and reduced to text for anchor names.
Compatibility adapters preserve literal `1)` guide
steps and Markdown immediately following a completed HTML paragraph spacer.

Parsing otherwise follows CommonMark/GFM rather than Blackfriday quirks.
Blank-line-separated quotations now render as separate blockquotes; this adds
spacing between adjacent Warning/Note sections in the consuming guide.
HTML whitespace and attribute order may change. Diff highlighting retains
addition/deletion/hunk styles but no longer emits the old intraline `.x` spans.
The stable heading-name helper is deliberately retained to avoid breaking links.

This server-side migration is independent of browser libraries, templates and
static assets. Roll back its commit to restore the previous engine; no content,
configuration, browser migration, or persisted-data conversion is required.

### Configuration and parser ownership

Startup loads a typed `config.Settings` once with unchanged configuration keys
and flag > environment > file > default precedence. Configuration files remain
optional; malformed files, booleans, and durations now fail startup explicitly.
With discovery enabled, refresh periods/timeouts must be positive, while informer
resync and initial delay may be zero but not negative.

Application components receive detached settings through their `WithConfig`
constructors. Viper remains the startup/legacy-constructor adapter rather than
live request state. Changes to Viper or caller-owned maps/slices after construction
do not reconfigure a running instance; restart to apply changes.

Embedding callers can create independent parsers with `spec.NewParser(settings)`,
then call `parser.LoadSuite()` for static files or `parser.Parse(location, raw)`
for discovered specifications. Status-code descriptions and static rewrite rules
belong to each parser. Invalid configured status-code tables fail construction
instead of leaving partially initialized shared state. Static-file rewrites still
do not apply to discovered parsing.

The legacy package-level `spec.Parse`/`spec.LoadSuite` entry points snapshot
configuration for each call. The obsolete `spec.APISuite`/`spec.APISuiteGroups`
globals have been removed; use the returned `spec.Suite` instead.
Rendering uses the local `render.HTMLOptions` type with the same `Layout` and
`Funcs` fields; `unrolled/render` is no longer required.

### Running DapperDox

Start up DapperDox, pointing it to your OpenAPI 2.0 specification file:

```
./dapperdox -spec-dir=<location of OpenAPI 2.0 spec>
```

DapperDox looks for the file `swagger.json` at the `-spec-dir` location, and builds reference documentation for the OpenAPI specification it finds. For example, the obligatory *petstore* OpenAPI specification is provided in the `examples/specifications/petstore` directory, so
passing parameter `-spec-dir=examples/specifications/petstore` will build the petstore documentation.

DapperDox will default to serving documentation from port 3123 on all interfaces, so you can point your
web browser at http://127.0.0.1:3123 or http://localhost:3123.

For an out-of-the-box example, execute the example run script:

```bash
./dapperdox \
  -spec-dir=examples/specifications/petstore/ \
  -bind-addr=0.0.0.0:3123 \
  -site-url=http://localhost:3123 \
  -spec-rewrite-url=petstore.swagger.io=PETSTORE.swagger.io \
  -log-level=trace \
  -force-specification-list=false \
  -theme=default  \
  -tls-certificate=server.rsa.crt \
  -tls-key=server.rsa.key \
  -author-show-assets=true \
  -assets-dir=./examples/overlay/assets \
  -proxy-path=/developer=https://developer.some-dev-site.com
```

This demonstrates many of the configuration options available. See [configuration](http://dapperdox.io/docs/configuration-guide).

## Discovery refreshes and health

Discovery publishes a complete documentation generation rather than appending
routes to a running router. In-flight requests finish on their original
generation. Unchanged specifications reuse the existing documentation; freshness
status can change without rebuilding routes.

An API that fails retrieval or parsing keeps its last known-good documentation
and displays a stale warning with its last successful refresh time. Other APIs
can still update. Confirmed service deletion removes its documentation, including
when the catalog becomes empty. Temporary fetch failures are not deletions.
Only one accepted version per active API is retained.

Packaged assets, themes, overlays and configuration are immutable until restart.
GFM rules and assets are compiled once; overlays reuse the rendering context.
Existing custom layouts receive stale warnings without requiring a template
change. Raw specification downloads retain their original JSON schema.

Health endpoints on the documentation listener:

* `GET /healthz`: process is responding.
* `GET /readyz`: discovery synchronized and a valid initial generation published.
  An explicitly partial or legitimately empty catalog is ready. An unavailable
  discovery API is not treated as an empty catalog.

Configure Kubernetes probes to use these endpoints rather than a rendered guide
page. A failed subsequent generation build retains the previous generation and
reports the failure instead of terminating the process.

Shutdown cancels in-flight specification requests and skips remaining discovery
fetches. Interrupted refreshes do not replace the last published discovery state.

Detailed current-state JSON is available at `GET /status` on the separate
management listener, default `127.0.0.1:8081`. Configure its address with
`--management-addr` or `MANAGEMENT_ADDR`; only literal loopback IPs are accepted.
Never add this port to the public Service, ingress or Envoy route.
Authorized operators can use Kubernetes port-forward:

```sh
kubectl -n <namespace> port-forward pod/<docs-pod> 18081:8081
curl --fail http://127.0.0.1:18081/status
```

Port-forward access is controlled by Kubernetes RBAC, not URL secrecy. Verify
that public requests and direct pod-IP requests cannot reach detailed status.
Public health responses intentionally do not disclose the API catalog.

### Release validation

Before deploying the fix through the consuming documentation repository:

1. Publish a traceable DapperDox image and pin that tested image in the consuming
   Dockerfile. Build/version output must identify the source revision.
2. Update liveness/readiness probes to `/healthz` and `/readyz`. Keep management
   access loopback-only and preserve the existing 1 GiB limit.
3. Run a 24-hour nonproduction soak with representative specifications, concurrent
   page/overlay traffic, genuine and unchanged API updates, retrieval failures,
   recovery, and confirmed deletions. Require zero OOMs/crashes, bounded
   route/rule/live-heap counts at equivalent catalog size, and peak docs-container
   memory below 768 MiB, including build overlap and old requests.
4. If capacity fails, investigate before changing limits. A low RSS immediately
   after restart or during an event-free period does not prove a leak is fixed.
5. Use the approved normal production rollout, then monitor for 24 hours.
   Stop/roll back on OOMs, incorrect publication, public status exposure,
   increasing retained memory at equivalent catalog size, or failure of the
   agreed memory/functional gates.

Rollback the compatible consuming docs release (image and probe configuration)
together. The old release is known to leak; rollback is not a permanent fix.
No deployment or soak is performed by running the unit tests.

## Acknowledgements

Many thanks to [Ian Kent](https://github.com/ian-kent) who spiked the Golang implementation of DapperDox
as part of a bigger project. His commit history was lost when DapperDox was extracted into a stand-alone
project, but its core retains his valuable work.

[David Mort](https://github.com/davidmort) for painstaking testing, and bug fixing.
