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

First build DapperDox (this assumes that you have your golang environment configured correctly):
```bash
go get && go build
```

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



