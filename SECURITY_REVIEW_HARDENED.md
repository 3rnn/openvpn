# Hardened differential security review

## Scope and disposition

Independent fork: `3rnn/openvpn`; upstream reference: `GavinTan/openvpn`.
Reviewed baseline: `cb632205b01b66551342e609e4a6b1bbf771c55f`.
Implementation reviewed through `44f472a7976447c7d5b12622963c3ad96e37ddc8`.
Find the enclosing documentation commit using `git rev-parse HEAD`; documentation cannot contain its own commit hash. No upstream merges were performed.

Release: `v1.0.0-hardened`, owner-approved on 2026-10-25 after the owner reported local deployment and successful client connection. Build commit/OCI revision: `35204c82885e87e1165fe1dda24a22f141ead303`; published digest: `sha256:2dc7fc01e3ffe7b458266cde4bc101e14b4f7063cbb5229bcb0bbf501a993c66`. Existing immutable version content is reused, not rebuilt. Documentation commits are recorded separately in release notes.

## Changes mapped to original findings

1. MFA: password verification creates server-session pending identity, expiry and attempt count; OTP reads a fresh database record, never client identity/secret. Pending sessions expire after three minutes, allow five failed attempts and rotate on completion. Failed lookup denies access.
2. Web rendering: untrusted fields use safe text/context-aware escaping; password display/export removed; mutable requests send session-bound CSRF tokens. Actual embedded UI exercised in Chromium.
3. Administrator bootstrap: random credentials in an exclusively created private file or explicit initialization environment. No public default, no password logging, forced first change. Legacy conversion cannot reset to public credentials.
4. Sessions: account/session versions revoke old sessions on local security changes. Custom persistence propagates datastore errors, transactionally verifies deletion and prevents revoked-row resurrection. Failure-injection tests cover logout and login/MFA rotation.
5. API disclosure: passwords/hashes and stored MFA seeds omitted. New seed exists only in the pending enrollment flow. Config response secrets are redacted; normal queries cannot retrieve old credentials.
6. Password storage: new/reset local passwords use bcrypt; successfully authenticated encrypted legacy passwords migrate. Unauthenticated legacy encrypted records remain migration debt; back up database and config together before upgrade.
7. CSRF: global unsafe-method middleware; POST logout; exact loopback/token service exemptions only. HttpOnly/SameSite cookies and Secure for configured HTTPS.
8. LDAP: verified LDAPS or mandatory StartTLS, CA file/directory support. Explicit insecure override is dangerous and not the default. Directory-side disable/password changes are not guaranteed to immediately invalidate LDAP Web sessions; directory-specific attributes/integration are needed.
9. PKI/runtime: private directories/files, atomic profile/CCD publication, serialized issuance, scoped identity/CN connection policies instead of global .ovip/.ovc. Existing volumes must be audited.
10. Configuration/commands: numeric management IDs; nft input allowlists; execution/plugin/include/path directives cannot be supplied as ordinary Web configuration. Fixed scripts and authority remain installation-owned.
11. Revocation: fail on CRL generation/verification errors, preserve recovery artifacts, permit retry of already-revoked entries; validate CA signature, validity and target serial before cleanup. Cleanup errors explicitly distinguish revoked from cleanup complete.
12. Persistence: staged settings documents, rollback and verified recovery snapshots; private atomic publication and parent-directory sync. This is not a multi-file crash-atomic transaction: interruption can require offline restoration.
13. Build: checked-in multi-stage Dockerfile, pinned official Go and Debian 13 base digests, module checksums, current source compilation. No upstream application binary or application image is used. Unsafe native installer is retired; upstream publishing automation disabled.

## Review and evidence

Multiple scoped read-only source reviews identified session-store error masking, CRL failure cleanup, management ID validation, CCD write permissions and settings partial publication. These findings were corrected in subsequent focused commits, with regression/failure injection tests rather than disabled controls.

Relevant executable checks:

- `go test ./...` and `go test -race ./...` run by implementation workers after final code changes; parent repeats final normal tests and vet before handoff.
- Chromium `node --test tests/frontend-security*.cjs`: 12 tests including real Go-handler integration.
- ShellCheck of entrypoint/auth/retired installer, `bash -n`, shell PKI test in isolated container.
- Canonical Dockerfile source build; no compiler/GCC/Git required in runtime.
- Prior canonical `744e155` acceptance: 89 passing runtime checks, including real login, bootstrap change, MFA, CSRF, session revocation, certificate issuance/revocation, stale/missing CRL recovery, TUN/UDP, and persistent-data restart/recreation. Final published-image acceptance repeated after the settings fix: 89 PASS, 1 NOT_TESTED; normal/race each 53 top-level tests plus 51 subtests, vet PASS, browser 12 PASS, ShellCheck 4 scripts/hooks PASS, actual EasyRSA and 4 runtime-shell tests PASS. Persistence compared 54 files exactly across restart/adoption/recreation.
- Parent Go vulnerability scan: no called or imported findings; one unused module-only advisory (OpenPGP) remains. This does not assess OS packages or browser vendor libraries.

Private, credential-free result summaries are stored outside Git under the local scratch acceptance directory. Test volumes contain synthetic credentials/keys and must not be committed or published. No external VPN handshake was performed: network-none testing deliberately has no connection to production networks. Local TUN/listener health does not prove external routing, NAT, MTU or client connectivity.

## Host test incident

An early shell regression incorrectly executed the legacy native installer on the host. It installed packages, compiled OpenVPN and downloaded an upstream application binary. This violated the intended test boundary. The application was not observed running; it was non-executable when inspected. Package installation started an empty Supervisor service.

The incident was disclosed. Following explicit owner authorization, Supervisor was stopped/disabled/removed, confirmed incident-new packages were removed after dependency simulation, and unowned newly installed files/downloads were quarantined with evidence. No claim of byte-for-byte restoration is possible without a pre-incident snapshot. This incident is not evidence that the container build uses the downloaded binary: the canonical builder compiles the source independently, and container tests use Debian-packaged OpenVPN. Future tests must never run installers on the host.

## Remaining risks and release gates

- The isolated harness did not test external VPN. The owner separately reported successful local deployment/client connection on 2026-10-25. Full DNS/routing/MTU/revocation coverage and real LDAP are not established by that report.
- Runtime root and Docker default capabilities plus NET_ADMIN remain; not privileged mode, not only NET_ADMIN. Never use host networking or mount Docker socket.
- Same authenticated identity/CN connections share the latest atomic policy; different identities are isolated.
- Signed Debian package resolution is mutable; pinned bases do not make full builds bit reproducible. Record installed packages and image digest.
- Frontend vendor/OS vulnerability inventory is separate from Go scanning.
- Cross-file settings publication has explicit recovery, not crash-atomicity.
- Legacy encrypted password records and LDAP directory-side revocation require operator attention.
- Git tag, registry version/digest and stable publication are separate gates. No stable while high-severity findings or required release gates remain unresolved.
