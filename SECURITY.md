# Security policy

## Supported versions and reporting

This independent GavinTan/openvpn fork has owner-approved release **v1.0.0-hardened** (2026-10-25), Linux/amd64. Image build commit: `35204c82885e87e1165fe1dda24a22f141ead303`; digest: `sha256:2dc7fc01e3ffe7b458266cde4bc101e14b4f7063cbb5229bcb0bbf501a993c66`. The owner reported successful local deployment and client connection; this is separate from network-none automated acceptance. No guaranteed support lifetime is announced.
The initial reviewed baseline is `cb632205b01b66551342e609e4a6b1bbf771c55f`.
Current source identity: run `git rev-parse HEAD` and record whether the tree is
clean. An image must record the exact clean source commit in its OCI revision.
Do not use a self-referential source hash in release documentation.

No private vulnerability-reporting address has been configured. Do not disclose
exploitable details or credentials in public issues. Ask the repository owner for
a private channel; if none exists, request one without including exploit details.
Until a release and maintenance window are explicitly announced, do not infer
production support from a tag name or a successful build.

## Threat model and deployment assumptions

* The container runs root plus NET_ADMIN for OpenVPN/TUN and nftables; a trusted
  administrator can affect VPN routing and PKI. Never use privileged/host-network
  mode, mount the Docker socket, or mount production host network configuration.
* `/data` is a secret-bearing root-owned volume: database, application secret,
  configuration, bootstrap credentials, CA/server/client private keys and profiles.
  Back it up offline with permissions/encryption; do not put it in Git or images.
* Management is loopback-only by default. Remote access needs a trusted TLS reverse
  proxy or SSH/VPN tunnel, correct trusted-proxy settings, and Secure cookies.
* LDAP uses verified TLS (including hostname) by default, TLS 1.2 minimum.
  `ldap://` must upgrade using StartTLS before bind. Mounted custom CA files are
  supported. The explicit insecure override logs a warning and is not a mitigation.
* Authentication and authorization must remain enabled. Credential failures,
  missing policy snapshots, malformed IPs and partial PKI fail closed.
* Do not execute arbitrary server/client configuration directives supplied through
  APIs. Only reviewed allowlisted data options are acceptable. Filesystem access,
  includes, plugins and scripts are not ordinary configuration data.

## Fix inventory and evidence

The implementation/change log describes intended fixes. The final security review
must record **actual passing tests and remaining findings** separately; a scaffold
is not an attestation. Required areas include MFA pending identity binding and
replay, session invalidation, admin initialization/migration, secret-free DTOs,
CSRF, safe DOM rendering, LDAP certificate rejection/trust, shell/nft injection,
PKI filesystem permissions, and concurrent connection isolation.

Runtime regression tests live in `tests/runtime_shell_test.py`,
`src/openvpn-web/ldap_tls_test.go`, `connection_config_test.go` and
`firewall_security_test.go`. They must run against private disposable data, with
no production firewall changes. Full integration additionally needs actual
OpenVPN startup/TUN, UDP, reconnect/persistence, login/MFA and revoke flows.

## Known limitations / security debt

* Final isolated acceptance: 89 PASS, external VPN NOT_TESTED by that harness.
  The owner separately confirmed local deployment and successful client connection
  and authorized release. Full dataplane/LDAP/OS-CVE coverage is not claimed.
* Signed Debian packages update over time; package repositories are not pinned to
  a snapshot. Record the final installed package inventory and scan the resulting
  image. A pinned base digest does not freeze subsequently installed packages.
* Go vulnerability scanning reports called/imported/module-only findings
  differently. The unused `golang.org/x/crypto/openpgp` package has no safe fix in
  its module (GO-2026-5932); the application does not import that package. This
  module-only advisory is not equivalent to a reachable application finding.
* Input validation for IPv6 shell configuration is syntactic, not full semantic
  subnet verification. Docker IPv6 networking requires separate review.
* Same authenticated username/certificate concurrent connections share the latest
  atomic identity policy. Different identities are isolated. Cleanup/retention of
  old `/data/connections` snapshots is an operator housekeeping task.
* Legacy reversible password-storage compatibility, if still present after auth
  integration, must be documented in the final review and migrated; API omission
  alone does not make reversible storage safe. Verify actual current auth code.
* Existing volumes/custom server configuration need manual audit before startup;
  data owned by untrusted local users is outside the supported trust boundary.
* Browser/static third-party libraries require separate inventory/version review;
  a clean Go scan does not assess JavaScript dependencies or OS packages.

## Release stop conditions

Publishing workflow is disabled and contains no third-party Actions. Do not
restore upstream image publishing or secret-consuming CI by accident. A registry
login is not authorization to push. Obtain the real username and explicit publish
approval separately; never ask for passwords/tokens in chat.

Before an immutable release: clean source review; relevant automated tests;
source-built image; container/application/OpenVPN startup; TUN/UDP validation;
authentication/MFA/authorization; persisted volume restart; certificate generation
and revocation; image inventory/metadata; secret scan; vulnerability review.
Known high-severity findings, auth bypasses or unmet gates mean **BLOCKED**. Do not
publish version, `latest`, or `stable` under those conditions. Do not auto-move
`stable` even after a successful build. Record commit, approved immutable image
tag and registry digest together only after approval.
