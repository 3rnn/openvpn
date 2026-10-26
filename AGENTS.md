# AGENTS.md

## Purpose

This repository is an independently maintained, security-hardened OpenVPN Web management project.

It originated from:

GavinTan/openvpn

The project has intentionally diverged from upstream and is maintained independently.

The primary goals are:

- provide a maintainable OpenVPN + Web UI deployment
- support Debian 13 as the primary host environment
- build all application binaries from reviewed source
- minimize supply-chain risk
- preserve strong authentication and session security
- avoid insecure defaults
- keep Docker deployment reproducible and auditable
- maintain clear release traceability

This repository is not an official GavinTan/openvpn release.

Security fixes take precedence over feature parity with the original upstream project.

---

# 1. Project Maintenance Model

This project is independently maintained.

Do not assume that upstream changes should be merged.

Upstream is reference-only.

Do not automatically:

```bash
git merge upstream/main
git rebase upstream/main
git pull upstream main
```

External code may only be introduced after manual review.

If a useful upstream change is discovered:

1. inspect the exact diff
2. understand the purpose
3. evaluate security impact
4. port only the required changes
5. add or update tests
6. verify existing hardened behavior is preserved
7. document the imported change

Prefer manual porting or selective cherry-picking over broad merges.

---

# 2. Project Origin

Original project:

```text
GavinTan/openvpn
```

Initial reviewed baseline:

```text
cb632205
```

The original LICENSE and copyright notices must remain intact where required.

Do not:

- remove original licensing information
- imply that this project is an official upstream release
- hide the historical origin of the project

The README should clearly state that this is an independently maintained fork.

---

# 3. Primary Target Environment

Primary deployment target:

```text
Debian 13
Docker Engine
Docker Compose
OpenVPN
```

The application should remain containerized unless a strong technical reason exists to change this architecture.

The preferred production model is:

```text
Debian 13 host
    ↓
Docker / Docker Compose
    ↓
OpenVPN + Web UI container
    ↓
persistent volumes
```

---

# 4. Security Baseline

The original audited baseline identified several security concerns.

Important previously identified issues include:

- MFA second-stage authentication/session binding weakness
- stored XSS affecting administrator sessions
- insecure default administrator credentials
- mutable latest binary downloads during Docker build
- weak session invalidation
- insufficient CSRF protection
- LDAP TLS certificate verification issues
- sensitive information exposure through APIs
- private-key permission issues
- predictable temporary-directory usage
- shared mutable state that may affect concurrent requests

These issues must not be reintroduced.

Any change touching authentication, sessions, user management, Docker builds, PKI, LDAP, API responses or OpenVPN configuration is considered security-sensitive.

---

# 5. Critical Security Rules

Agents MUST NOT:

- download and execute precompiled application binaries from the original project
- use yyxx/openvpn or other upstream application images as production runtime images
- use mutable `latest` application binaries
- silently introduce public default credentials
- use `admin/admin` as a production default
- return stored user passwords through APIs
- expose existing MFA secrets after enrollment
- expose CA private keys through normal application APIs
- expose LDAP bind passwords
- expose GitHub, Docker Hub or API tokens
- disable TLS verification by default
- disable CSRF protection to resolve compatibility issues
- use `privileged: true` without explicit technical justification and approval
- automatically change production host firewall rules
- automatically deploy to production servers unless explicitly instructed
- commit secrets or production credentials
- weaken authentication to make tests pass
- weaken authorization to make tests pass
- publish a stable release while known high-severity issues remain unresolved

If a task conflicts with these rules, stop and report the conflict.

---

# 6. Source-of-Truth Policy

The checked-out source repository is the source of truth for the application.

Production application binaries must be derived from the current Git commit.

Expected build chain:

```text
Git commit
    ↓
reviewed source
    ↓
compiler / builder stage
    ↓
application binary
    ↓
runtime Docker image
```

The following pattern is prohibited for project application binaries:

```bash
curl https://.../latest-binary
wget https://.../latest-binary
```

Do not download application executables during runtime image construction.

System packages may be obtained from trusted operating-system repositories.

Examples:

```text
Debian repositories
official language package registries
verified upstream dependencies
```

---

# 7. Docker Build Policy

Prefer multi-stage Docker builds.

Example architecture:

```text
builder stage
    ↓
compile application
    ↓
runtime stage
```

The builder stage may contain:

- compiler
- source code
- build utilities
- dependency cache
- package manager

The production runtime stage should contain only what is required to operate.

Typical runtime contents:

- application binary
- Web templates
- static assets
- OpenVPN
- EasyRSA
- iproute2
- required firewall utilities
- CA certificates
- timezone data
- required runtime libraries

Do not leave unnecessary build components in the production image.

Avoid including:

```text
Git
Go compiler
GCC
Node build environment
full source tree
temporary build artifacts
package caches
```

---

# 8. Base Image Policy

Preferred runtime base:

```text
debian:13-slim
```

If this cannot be used, document:

- why it is incompatible
- what alternative image was selected
- why that image is trusted
- what security or maintenance implications exist

Do not replace the runtime base image merely for convenience.

---

# 9. Docker Versioning

Do not rely on mutable application image tags for production deployments.

Preferred release tags:

```text
v1.0.0-hardened
v1.0.1-hardened
v1.1.0-hardened
```

A `stable` tag may exist, but it is mutable and should only point to an explicitly approved release.

Avoid using:

```text
latest
```

for deployment automation.

Version tags are immutable.

Once released, do not silently rebuild a version tag with different content.

---

# 10. OCI Metadata

Production images should contain useful OCI metadata where practical.

Recommended labels:

```text
org.opencontainers.image.title
org.opencontainers.image.description
org.opencontainers.image.source
org.opencontainers.image.revision
org.opencontainers.image.version
org.opencontainers.image.licenses
```

The revision should correspond to the exact Git commit used to build the image.

---

# 11. Container Privileges

Apply the principle of least privilege.

Preferred capability:

```yaml
cap_add:
  - NET_ADMIN
```

If the TUN device is required:

```yaml
devices:
  - /dev/net/tun:/dev/net/tun
```

Avoid:

```yaml
privileged: true
```

Any additional Linux capability must be justified.

Do not grant capabilities simply because deployment is easier.

---

# 12. Host System Safety

Agents must not automatically change the production host.

Do not automatically modify:

```text
iptables
nftables
ufw
firewalld
sysctl
SSH configuration
Docker daemon configuration
network interfaces
host routing
```

unless explicitly requested.

When host changes are necessary, present the exact required changes separately.

---

# 13. Network Exposure

OpenVPN public endpoint may be exposed as required.

Typical endpoint:

```text
1194/udp
```

The Web administration interface should not be publicly exposed by default.

Preferred Web UI access paths:

```text
LAN
VPN
localhost
reverse proxy
specific trusted management networks
specific trusted management IPs
```

Do not assume that a management port should listen publicly on:

```text
0.0.0.0
```

Document exposure clearly.

---

# 14. Persistent Data

Important state must survive container recreation.

Persist:

- application database
- OpenVPN server configuration
- EasyRSA PKI
- CA
- server certificate
- server private key
- client certificates
- client private keys where required
- application configuration
- security configuration

Do not rely on the container writable layer for persistent state.

Before changing volume paths, consider upgrade and rollback compatibility.

---

# 15. Secret Management

Never commit secrets.

Examples:

```text
.env
production passwords
admin passwords
LDAP bind passwords
private keys
CA private keys
client private keys
GitHub tokens
Docker Hub tokens
API tokens
production certificates containing secret material
```

Provide:

```text
.env.example
```

when configuration examples are required.

Example values must be placeholders.

Never put real credentials into:

- README
- Dockerfile
- docker-compose.yml
- Git history
- tests
- examples
- shell scripts
- CI logs

---

# 16. Administrator Initialization

Production deployments must not create public default credentials.

Do not create:

```text
admin / admin
```

Preferred administrator initialization mechanisms:

1. secure random initial password
2. explicit environment variable
3. controlled first-run bootstrap process

A migration failure must never silently reset an administrator password to a public default.

If administrator initialization cannot safely complete:

fail safely and provide a clear recovery procedure.

---

# 17. Password Handling

Existing passwords must not be returned to the frontend.

Supported actions should be:

```text
set password
change password
reset password
```

Do not implement:

```text
show existing password
retrieve decrypted password
export passwords
```

Preferred storage should use an appropriate password hashing algorithm.

Examples:

```text
Argon2id
bcrypt
scrypt
```

If legacy reversible encryption remains temporarily, document it clearly as security debt.

Do not expose decrypted passwords through APIs.

---

# 18. MFA Security

MFA authentication must be server-authoritative.

First-stage authentication should establish the user identity on the server.

The server-side session should contain information such as:

```text
authenticated_user_id
mfa_pending
mfa_pending_expiry
```

The second MFA step must not trust the client to choose the identity being verified.

Do not trust client-provided:

```text
user ID
MFA secret
MFA seed
username-to-secret mapping
```

For MFA verification:

1. obtain authenticated user ID from server session
2. load MFA configuration from database
3. validate OTP
4. reject database lookup failures
5. complete authentication
6. clear temporary MFA state
7. rotate session if appropriate

MFA pending state must be:

- session-bound
- short-lived
- single-user
- resistant to cross-session reuse

---

# 19. MFA Secret Exposure

An MFA seed may be shown only when required during enrollment.

Once enrollment is complete:

do not expose the existing full MFA seed through ordinary APIs.

If MFA must be reset:

generate a new secret.

Do not retrieve and display the previous secret.

---

# 20. Session Security

Sessions must be invalidated after security-sensitive account changes.

Examples:

- password change
- account disable
- MFA reset
- privilege change
- administrator forced logout
- account deletion

A recommended design is a server-side session version.

Example:

```text
user.session_version
session.session_version
```

If these values differ:

the session is invalid.

Session cookies should use appropriate controls where supported:

```text
HttpOnly
SameSite
Secure
```

Use Secure cookies when HTTPS is in use.

---

# 21. XSS Prevention

Treat all user-controlled input as untrusted.

Avoid rendering untrusted values using:

```javascript
innerHTML
```

Avoid unsafe string interpolation into HTML.

Prefer:

```javascript
textContent
```

or equivalent safe DOM methods.

User-controlled fields may include:

- username
- display name
- description
- notes
- remarks
- LDAP fields
- imported data
- profile fields
- arbitrary metadata

Do not assume an authenticated user is trusted.

---

# 22. CSRF Protection

State-changing Web actions must use CSRF protection.

This includes:

```text
POST
PUT
PATCH
DELETE
```

Security-sensitive examples:

- create user
- modify user
- delete user
- reset password
- configure MFA
- create certificate
- revoke certificate
- change OpenVPN configuration
- restart OpenVPN
- modify LDAP configuration
- change administrator settings

Do not remove CSRF protection to fix frontend failures.

Update the frontend to provide valid CSRF tokens.

---

# 23. LDAP Security

LDAP configurations must use secure defaults.

For:

```text
ldap://
```

support StartTLS where possible.

For:

```text
ldaps://
```

server certificate verification must be enabled by default.

If an insecure override exists, it must be explicit.

Example:

```text
LDAP_TLS_INSECURE_SKIP_VERIFY=false
```

The default must remain:

```text
false
```

Custom CA support should be available where practical.

Examples:

```text
LDAP_CA_FILE
LDAP_CA_DIR
```

---

# 24. API Sensitive Data Policy

Review API responses for unnecessary sensitive information.

Normal API responses must not expose:

```text
passwords
password hashes
MFA secrets
CA private keys
server private keys
client private keys
LDAP bind passwords
administrator credentials
application secrets
tokens
```

Return only what the client actually needs.

Sensitive administrative actions should favor reset/regeneration rather than disclosure.

---

# 25. PKI File Permissions

Private key material must have restrictive permissions.

Recommended private-key mode:

```text
0600
```

Recommended sensitive-directory mode:

```text
0700
```

Verify actual permissions after:

- EasyRSA operations
- certificate generation
- CA initialization
- key import
- key restore
- backup restore

Do not assume generated permissions are secure.

---

# 26. Temporary Files

Sensitive temporary files must use unpredictable locations.

Prefer:

```bash
mktemp -d
```

or language-standard secure temporary APIs.

For Go:

```text
os.MkdirTemp
os.CreateTemp
```

Avoid predictable locations such as:

```text
/tmp/openvpn
/tmp/config
/tmp/user.ovpn
```

Cleanup must happen on:

- success
- error
- cancellation where practical

---

# 27. Concurrent Request Safety

Avoid shared mutable state across users and requests.

Review global variables used for:

- login state
- MFA state
- certificate generation
- OpenVPN configuration generation
- temporary paths
- user configuration
- exported profiles
- application settings

Per-user and per-request state should be isolated.

Do not use shared mutable configuration buffers when simultaneous requests may occur.

---

# 28. Command Execution

Any code invoking system commands is security-sensitive.

When executing:

```text
openvpn
easyrsa
openssl
iptables
ip
system utilities
```

avoid shell interpolation wherever possible.

Prefer structured process execution.

For Go:

prefer:

```text
exec.Command
```

with separate arguments.

Avoid constructing:

```bash
sh -c "<user controlled string>"
```

Never allow user-controlled values to become command fragments without strict validation.

---

# 29. OpenVPN Configuration Editing

The Web UI may manage OpenVPN configuration, but configuration changes are highly privileged.

Validate user input.

Do not allow arbitrary command execution through configuration fields.

Preserve safe configuration backups before destructive changes where practical.

Configuration reload/restart operations should require appropriate administrator authorization.

---

# 30. Authorization

Authentication does not imply authorization.

Administrative endpoints must explicitly verify administrator privileges.

Do not rely only on:

```text
logged-in user
```

for privileged operations.

Review authorization for:

- users
- groups
- certificates
- OpenVPN configuration
- service restart
- LDAP configuration
- system settings
- logs containing sensitive information
- backups
- exports

---

# 31. Input Validation

Validate externally supplied data.

Examples:

- usernames
- IP addresses
- CIDR ranges
- port numbers
- filenames
- certificate names
- group names
- LDAP URLs
- DNS addresses
- route definitions
- timeout values

Avoid using unchecked user input as:

- filesystem paths
- shell arguments
- template HTML
- database query fragments
- network destinations

---

# 32. File Path Safety

Operations involving file paths must prevent path traversal.

Reject unsafe patterns such as:

```text
../
../../
```

Do not trust filenames supplied by the client.

Use controlled base directories and normalized paths.

Sensitive files must remain within intended storage locations.

---

# 33. Logging Policy

Logs must help troubleshooting without leaking secrets.

Do not log:

```text
passwords
MFA seeds
private keys
API tokens
LDAP bind passwords
session cookies
authentication headers
full secret environment variables
```

Authentication failures may log:

- user identifier
- timestamp
- source information where appropriate
- failure category

Do not log secret values.

---

# 34. Error Handling

Fail securely.

Do not reveal unnecessary implementation details to unauthenticated users.

Examples of sensitive information:

- filesystem paths
- stack traces
- database credentials
- LDAP passwords
- internal tokens

Detailed errors may be written to server logs if they do not contain secrets.

User-facing errors should remain concise.

---

# 35. Dependency Management

Prefer explicitly versioned dependencies.

Before upgrading major dependencies:

1. inspect release notes
2. identify security impact
3. identify breaking changes
4. run tests
5. build Docker image
6. test authentication
7. test OpenVPN startup

Do not perform broad dependency upgrades unrelated to the current task unless requested.

---

# 36. External Downloads

External downloads during build should be minimized.

Permitted examples may include:

- Debian packages
- Go modules
- verified language dependencies

Application runtime binaries should not be downloaded from unverified release URLs.

Where practical, dependency integrity should use:

- lock files
- Go module checksums
- package repository signatures
- pinned versions
- image digests

---

# 37. Testing Policy

Changes are not complete until relevant tests pass.

Run existing tests.

For Go projects, typically:

```bash
go test ./...
```

Security fixes should include regression tests whenever practical.

Important regression areas include:

- MFA identity binding
- MFA secret trust boundary
- failed MFA database lookup
- session invalidation
- password disclosure
- MFA secret disclosure
- XSS handling
- CSRF enforcement
- administrator initialization
- LDAP certificate verification
- PKI file permissions
- authorization
- concurrent request isolation

---

# 38. Security Regression Tests

Important tests should verify at minimum:

1. User B cannot use User A's pending MFA authentication.
2. Client-provided user ID cannot change MFA identity.
3. Client-provided MFA secret is ignored.
4. Failed database lookup causes authentication failure.
5. Stored XSS payload is rendered safely.
6. Existing passwords are not returned through APIs.
7. Existing MFA seed is not returned after enrollment.
8. Public default admin credentials are not created.
9. Password migration failure does not reset to admin/admin.
10. Disabled accounts lose active sessions.
11. Password changes invalidate previous sessions.
12. CSRF protection blocks unauthorized state-changing requests.
13. LDAPS verifies server certificates by default.
14. Private keys use restrictive filesystem permissions.
15. Sensitive API responses do not expose secret material.

---

# 39. Docker Validation

Before a release, validate the container.

Typical commands:

```bash
docker build .
docker compose config
docker compose up -d
docker compose ps
docker compose logs
```

Verify:

- application starts
- OpenVPN starts
- Web UI starts
- TUN device works
- UDP port is listening
- persistent data survives restart
- login works
- user management works
- certificate generation works
- certificate revocation works
- MFA works
- OpenVPN configuration persists

---

# 40. Test Environment Safety

Testing must not unintentionally alter production systems.

Do not:

- connect test containers to production networks unless explicitly requested
- overwrite production PKI
- reuse production CA private keys
- import production secrets into tests
- modify production firewall rules
- restart production OpenVPN services

Use isolated test data where practical.

---

# 41. Git Workflow

Prefer small, focused commits.

Examples:

```text
fix(auth): bind MFA verification to authenticated session
fix(web): prevent stored XSS
fix(session): invalidate stale sessions
fix(ldap): enforce TLS verification
fix(pki): restrict private key permissions
build: compile application from source
build: add Debian 13 runtime image
docs: document hardened deployment
```

Avoid unrelated changes in one commit.

Do not rewrite published release history unless explicitly requested.

---

# 42. Branch Policy

Suggested branch model:

```text
main
feature/*
fix/*
security/*
release/*
```

Security fixes should preferably use:

```text
security/<description>
```

or:

```text
fix/<description>
```

Avoid direct large experimental changes on `main`.

---

# 43. Release Policy

A release should correspond to:

```text
Git commit
Git tag
Docker image tag
Docker image digest
```

These must be traceable to one another.

Preferred version format:

```text
v1.0.0-hardened
```

A release is not complete merely because Docker build succeeds.

---

# 44. Stable Release Rules

The `stable` Docker tag may only point to a release that has passed:

- source review
- relevant automated tests
- Docker build
- Docker startup validation
- authentication validation
- security regression tests
- persistent-data validation

If a known high-severity security issue remains:

do not publish or move `stable`.

Report:

```text
BLOCKED
```

and explain why.

---

# 45. Image Publishing

Before pushing an image:

1. confirm Docker authentication
2. confirm repository name
3. confirm version tag
4. confirm Git commit
5. build locally
6. run tests
7. start container
8. validate service
9. inspect final image
10. push image
11. record digest

Do not print Docker Hub credentials.

---

# 46. GitHub Publishing

Before pushing:

```bash
git status
git remote -v
git branch --show-current
git rev-parse HEAD
```

Ensure secrets are not staged.

Review:

```bash
git diff --cached
```

Do not commit credentials.

Do not expose private tokens in command history, scripts or documentation.

---

# 47. Release Documentation

Maintain where appropriate:

```text
README.md
CHANGELOG.md
SECURITY.md
AGENTS.md
SECURITY_REVIEW_HARDENED.md
```

Each release should document:

- version
- Git commit
- major changes
- security fixes
- compatibility notes
- migration requirements
- Docker image reference
- remaining known risks

---

# 48. SECURITY.md

`SECURITY.md` should describe:

- supported versions
- how vulnerabilities should be reported
- known security assumptions
- unresolved security debt
- security-sensitive deployment guidance

Do not use AGENTS.md as a replacement for SECURITY.md.

---

# 49. CHANGELOG.md

Security changes should be clearly documented.

Example:

```text
## v1.0.0-hardened

### Security

- fixed MFA session binding weakness
- removed insecure default administrator password
- prevented stored XSS
- removed sensitive credential exposure
- enforced LDAP certificate verification
- added CSRF protection
- restricted PKI key permissions

### Build

- removed precompiled upstream binary downloads
- application is now built directly from source
- added Debian 13 runtime image
```

---

# 50. Backup and Restore

Changes affecting persistent data must consider backups.

Before database or PKI migrations:

- identify backup requirements
- document backup command
- document restore command
- avoid destructive migration without recovery path

CA private keys are critical assets.

Do not regenerate them casually.

---

# 51. Migration Safety

Database or configuration migrations must be:

- deterministic
- documented
- recoverable where practical
- resistant to silent data loss

Migration failure must fail visibly.

Do not silently reset security settings to insecure defaults.

---

# 52. Compatibility

Preserve existing functionality where reasonable.

However:

security fixes take precedence over backward compatibility with insecure behavior.

Examples:

If an old client expects passwords to be returned through an API:

do not preserve that behavior.

If an old LDAP setup requires certificate verification to be disabled:

require explicit insecure override.

---

# 53. Performance Changes

Do not optimize security-sensitive code without understanding correctness.

Authentication, session handling and PKI correctness take priority over micro-optimizations.

Performance changes should not introduce shared state that weakens user isolation.

---

# 54. Code Review Priorities

When reviewing changes, prioritize:

1. authentication
2. authorization
3. secrets
4. session handling
5. command execution
6. filesystem access
7. Docker build integrity
8. PKI handling
9. Web security
10. concurrency
11. network exposure
12. maintainability

---

# 55. Agent Working Method

When an agent works in this repository:

1. inspect the repository before editing
2. understand the relevant code path
3. identify security impact
4. make the smallest safe change
5. preserve unrelated behavior
6. add tests where appropriate
7. run tests
8. validate container build when relevant
9. review the final diff
10. document important changes
11. report remaining risks

Do not modify code blindly.

---

# 56. Agent Reporting Requirements

After completing meaningful changes, report:

- files changed
- reason for each change
- tests executed
- test results
- Docker build result if relevant
- compatibility impact
- security impact
- remaining risks
- manual steps required

Distinguish:

```text
verified
assumed
not tested
```

Do not present assumptions as verified facts.

---

# 57. Stop Conditions

Stop and report before proceeding if:

- a requested change requires disabling major security controls
- a build unexpectedly downloads an unreviewed application binary
- secrets are found committed in the repository
- production credentials are discovered
- a high-severity vulnerability remains unresolved before a stable release
- tests reveal authentication or authorization bypass
- privileged container mode becomes necessary without clear reason
- required behavior is ambiguous and could create a security regression

Do not conceal blockers in order to finish a task.

---

# 58. Definition of Done for Code Changes

A code change is complete only when:

- implementation is finished
- relevant tests pass
- security implications are considered
- no secrets are introduced
- documentation is updated if needed
- final diff is reviewed
- remaining risks are reported

---

# 59. Definition of Done for Docker Changes

A Docker change is complete only when:

- application is built from checked-out source
- no unreviewed application binary is downloaded
- build succeeds
- container starts
- OpenVPN starts
- Web UI starts
- required volumes persist
- required ports behave as expected
- privileges are minimized
- image metadata is correct
- image version is traceable to Git commit

---

# 60. Definition of Done for Releases

A release is complete only when the following can be traced together:

```text
Git commit
    ↓
Git tag
    ↓
Docker image version
    ↓
Docker image digest
```

The release must also have:

- passing relevant tests
- successful Docker validation
- release notes
- known-risk documentation
- no unresolved release-blocking high-severity issue

---

# 61. Independent Maintenance Principle

This repository is not required to remain compatible with the development direction of the original project.

Do not prioritize upstream parity over:

- security
- auditability
- maintainability
- reproducibility
- deployment stability

External code is reference material, not an automatic source of truth.

The project should evolve based on its own requirements.

---

# 62. Final Principle

When choosing between convenience and security:

choose the safer implementation.

When choosing between automatic upstream synchronization and controlled review:

choose controlled review.

When choosing between precompiled binaries and reproducible source builds:

choose reproducible source builds.

When uncertain about security-sensitive behavior:

stop, investigate, test and report rather than guessing.

## UI/UX Design Rules

All frontend UI development and modifications must follow
the design specifications defined in ./DESIGN.md.

Before making any UI changes, read DESIGN.md.

Maintain consistent layouts, typography, spacing,
colors, components, and interaction patterns.
