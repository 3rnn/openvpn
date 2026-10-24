# Changelog

## Unreleased — v1.0.0-hardened candidate (NOT published)

Origin: independent fork of GavinTan/openvpn at reviewed baseline
`cb632205b01b66551342e609e4a6b1bbf771c55f`. Preserve MIT copyright. Upstream is
reference-only. Determine the current source commit with `git rev-parse HEAD`;
release/image identity belongs in the final validation report after commits, not
an impossible self-hash embedded in this commit.

### Security / runtime

* Session persistence distinguishes absent rows from datastore read failures;
  logout/rotation verifies deletion transactionally before clearing the old ID.
  Failed reads/writes do not mint replacement sessions or resurrect revoked rows.
  Fault-injection regressions cover logout, password-to-MFA and login completion.

* LDAP: verify TLS chain and hostname by default, minimum TLS 1.2; mandatory
  StartTLS on `ldap://` before credentials; explicit insecure override warning;
  system roots plus `LDAP_CA_FILE`/`LDAP_CA_DIR`; fail closed on malformed CA/bool.
* Close LDAP connections on bind/search/auth failure. Reject unsafe URL schemes.
* Replace global static-IP/group configuration handoff with atomic 0600 snapshots
  scoped to authenticated username plus certificate, and per-connection hook
  files. Allowlist data directives; scripts/plugins/includes are not accepted.
* Private PKI umask, directory/file permission repair after EasyRSA operations;
  serialize shared EasyRSA index mutation; no overwrite of a partial existing CA.
* Validate shell identities, explicit remote endpoint/ports/protocols, management
  loopback and nft input grammar (addresses/CIDRs/ranges/names/policy/QoS).
* Fixed loopback hook endpoints, URL-encoded form fields, bounded curl timeouts,
  no secret-bearing response logging; no arbitrary authentication-disable action.
* Secure unpredictable temporary files with cleanup traps and atomic publication.

### Build / deployment

* Build application and embedded assets from checked-out source with explicit
  official Go 1.27.3 digest-pinned builder; Debian 13 slim digest-pinned runtime.
* Remove mutable upstream executable and EasyRSA archive downloads. Trusted
  signed Debian packages supply OpenVPN/EasyRSA/system tools.
* Retire unsafe host installer with a non-mutating failure and migration guidance.
* Disable tag-driven upstream publishing/third-party Actions/credential-consuming
  CI; workflow is manual no-op only. No image/tag/push performed by this change.
* Compose requires `<DOCKERHUB_USERNAME>/openvpn:v1.0.0-hardened`, loopback
  management, explicit TUN/NET_ADMIN, persistent `/data`, no hardcoded secrets.
* Security-targeted Go module upgrades remove called/imported advisories as
  checked by govulncheck; transitive requirements raise module Go minimum to 1.26.
  Build compiler remains explicitly 1.27.3.

### Integration areas (final acceptance required)

Authentication/frontend agents separately address MFA identity trust, session
invalidation, CSRF, administrator bootstrap/migration, secret response DTOs,
server configuration authorization/allowlisting and XSS-safe DOM rendering. The
final review must enumerate verified fixes, actual tests and unresolved issues;
these topics are not claimed complete by this scaffold.

### Compatibility / debt

Plaintext-only LDAP, insecure certificates, arbitrary OpenVPN executable
configuration directives, path-like certificate names, disabled authentication,
implicit remote-host detection and the bare-metal installer are intentionally
unsupported. Back up existing volumes, audit custom configuration and migrate
credentials without public default resets. See SECURITY.md for OS-package/static
asset review, module-only OpenPGP advisory, root-container trust model, same-
identity policy semantics and final release gates. No stable release is permitted
while known high-severity findings or required validation gaps remain.
