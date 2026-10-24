# OpenVPN Web — independent hardened fork

This independently maintained fork originates from **GavinTan/openvpn**, reviewed
baseline `cb632205b01b66551342e609e4a6b1bbf771c55f`. It is not an official upstream
release. The original MIT license and copyright remain in `LICENSE`. Upstream is
reference-only: manually review and port individual changes, never auto-sync.

**Release status: UNRELEASED / BLOCKED pending final integration, container and
security acceptance tests.** No image has been published by this work. Do not use
`latest` or move `stable`; a known release-blocking high finding forbids release.
See `SECURITY.md`, `CHANGELOG.md` and the final security review for actual results.

## Deployment model

Primary target: Debian 13, Docker Engine and Compose, an explicitly mapped TUN
device and a private persistent `/data` volume. Administration binds to host
`127.0.0.1:8833`; UDP `1194` is public. Use an SSH tunnel or a trusted HTTPS reverse
proxy, and configure HTTPS/secure session settings before remote administration.
Do not publish port 8833 to the internet. Do not use host networking or privileged
mode. NET_ADMIN and root are required for TUN/nftables inside the container;
this is not a sandbox for untrusted administrators. Host routing/firewall setup
must be separately reviewed and explicitly authorized, not run by this project.

The old bare-metal installer is **retired** and exits without installing anything.
It previously fetched mutable upstream binaries and altered host configuration.
Only build from this reviewed repository. Existing upstream images are not a
supported migration mechanism.

## Build from the exact checked-out source

Run at the repository root, with a clean reviewed commit:

```sh
git status --short
git rev-parse HEAD
REV=$(git rev-parse HEAD)
docker build -f build/Dockerfile \
  --build-arg VCS_REF="$REV" --build-arg VERSION=unreleased \
  -t openvpn-local:v1.0.0-hardened .
docker image inspect openvpn-local:v1.0.0-hardened --format '{{json .Config.Labels}}'
```

The Go compiler is explicitly `1.27.3` in an official digest-pinned builder;
the runtime is official digest-pinned `debian:13-slim`. The application is compiled
from `src/openvpn-web` with embedded templates/static assets. No upstream
application executable/image is used. Signed Debian repositories supply OpenVPN,
EasyRSA and utilities. Package versions are not snapshot-pinned: rebuilds receive
security updates and are not bit-for-bit reproducible. Record package inventory
and the resulting image digest for each approved immutable release.

`docker-compose.yml` intentionally requires a username; the intended release
reference is **`<DOCKERHUB_USERNAME>/openvpn:v1.0.0-hardened`**. Do not guess a
namespace or assume this image exists. A local test override avoids any registry:

```yaml
# compose.local.yml — local validation only; do not publish it
services:
  openvpn:
    image: openvpn-local:v1.0.0-hardened
    pull_policy: never
```

For config validation set `DOCKERHUB_USERNAME` to the literal local-only value
`placeholder` (not a publishing destination), then use the override:

```sh
install -d -m 0700 data
DOCKERHUB_USERNAME=placeholder docker compose -f docker-compose.yml -f compose.local.yml config
# Only after reviewing host networking/TUN and completing security tests:
DOCKERHUB_USERNAME=placeholder docker compose -f docker-compose.yml -f compose.local.yml up -d
```

No credentials belong in Compose, build arguments, image layers or Git. Bootstrap
administrator access must use the hardened private-file/explicit initialization
flow; never restore public default credentials, never print passwords in logs.
Treat the generated bootstrap credential file (if enabled by the application) as
a secret: retrieve privately, change the password, then securely remove the file.

## LDAP

Configure LDAP URL, base/bind DN, user attributes and bind password through the
protected settings interface. The CA controls below are independent runtime
**environment** options (not fields sent by the browser):

* `ldaps://directory.example:636`: verifies chain and hostname, TLS 1.2 minimum.
* `ldap://directory.example:389`: **mandatory StartTLS before any bind**; no
  plaintext fallback. Servers without StartTLS must be upgraded or use LDAPS.
* `LDAP_CA_FILE=/run/ldap-ca/ca.pem`: read-only mounted PEM trust bundle.
* `LDAP_CA_DIR=/run/ldap-ca`: directory of PEM certificate files. Every file must
  contain valid certificates; empty directories or unreadable files fail closed.
  System roots remain trusted. Never mount private keys in this directory.
* `LDAP_TLS_INSECURE_SKIP_VERIFY=false` is the default. Explicit `true` is an
  emergency insecure override and produces a warning. It permits interception of
  directory credentials; do not use it as a production fix.

VPN authentication policy snapshots are atomic mode-0600 files under
`/data/connections`, keyed by SHA256(username + NUL + certificate common name).
The OpenVPN hook reads the authenticated identity's policy and writes its own
per-connection target atomically. No global `.ovip`/`.ovc` handoff is supported.
Concurrent connections sharing the same identity/certificate intentionally share
that identity's current policy; different identities never share a snapshot.
Only allowlisted data-plane client directives are accepted. Arbitrary scripts,
plugins, includes and filesystem directives are intentionally incompatible.

## PKI and migration

Stop the old service and make a complete private offline backup before migrating.
Preserve `/data` including database, configuration, PKI, clients and CCD. Do not
regenerate an existing CA. Partial PKI initialization fails closed and requires
recovery; it must not silently replace an existing key. The entrypoint uses
umask 077 and enforces PKI directories 0700 and files/private profiles 0600,
including after actual EasyRSA issuance and renewal. Client names must be safe
identifiers; remote endpoint and port must be explicitly supplied. Certificate
issuance is serialized with `flock` because EasyRSA shares an index.

Example backup/restore (operator-run, with the service stopped; keep archives
outside the repository and encrypt them at rest):

```sh
umask 077
tar -C . -cpf /secure/offline/openvpn-data.tar data
# Restore to a clean, stopped deployment only; never over a running database.
tar -C /new/deployment -xpf /secure/offline/openvpn-data.tar
chmod 0700 /new/deployment/data
```

Review legacy custom server/client configuration before restart. Unsafe executable
directives must be removed, not grandfathered. Upstream settings, TLS directories,
old static-IP handoffs and default credentials may require explicit migration.
IPv6 deployment needs separately reviewed Docker network and forwarding settings;
IPv6 syntax checking is narrower than complete semantic validation.

## Validation and publishing

```sh
python3 tests/runtime_shell_test.py
# Use private disposable data; the application initializes state during Go tests.
TEST_DATA=$(mktemp -d)
trap 'rm -rf -- "$TEST_DATA"' EXIT INT TERM
(cd src/openvpn-web && OVPN_DATA="$TEST_DATA" GOTOOLCHAIN=go1.27.3 go test ./...)
(cd src/openvpn-web && GOTOOLCHAIN=go1.27.3 govulncheck ./...)
```

Use a private configured temporary base (`TMPDIR`) when running tests. Never run
legacy installers as a regression test. Validate source review, authentication,
MFA/session/CSRF/XSS regressions, certificate issuance/revocation, OpenVPN/TUN/UDP
startup, persistence and image metadata before any release. A successful compile
or no called Go vulnerabilities does not prove production readiness.

Publishing CI is disabled: manual no-op workflow, no third-party Actions and no
credential access. A user-provided namespace, explicit push approval and all
release gates are required; Docker login alone is not push approval. Do not tag or
push `latest`/`stable`. After approval record commit, immutable tag and registry
digest together. Determine the current commit using `git rev-parse HEAD`; do not
place a self-referential fabricated commit hash inside a commit.
