# Frontend security checks

Run from the repository root with Node.js, Go, Playwright, and its Chromium browser installed:

```sh
NODE_PATH=/path/to/node_modules node --test tests/frontend-security*.cjs
```

The reviewed run used Playwright 1.64.1 and its installed Chromium. Dependencies are test-only; no vendor library or production dependency is changed.

## Verified coverage

- Real browser execution of the checked-out JavaScript, jQuery, Bootstrap, DataTables, and templates.
- Literal XSS payload text in user rows/details, groups, dropdowns/options, remarks, certificate fields, and notifications; no injected image/event-handler elements or execution.
- Quoted attribute preservation and second-parse popover escaping; executable/cross-origin client download URLs are rejected.
- Existing passwords and MFA seeds are not rendered, copied, or loaded into settings fields. User export remains available but depends on the backend's password-free export contract.
- Shared mutations fail closed without a CSRF token, use same-origin credentials and `X-CSRF-Token`, preserve multipart boundaries, and refresh tokens after login/MFA and logout. Login/MFA forms and the actual logout anchor execute these paths.
- Current-password reauthentication fields for settings, MFA enrollment/disable, and administrator MFA resets. Settings credentials are cleared on close; changing administrator credentials returns to login, never GET logout.
- MFA status responses never populate a seed. Only explicit pending enrollment can show its new QR/seed; modal closure and successful enrollment clear them.
- The login page follows the backend `user.isFirstLogin` flag for administrator/user forced password changes and returns to login after session revocation.
- LDAP UI documents mandatory StartTLS, verified LDAPS, custom CA configuration, and the danger of the explicit environment-only insecure override. It adds no UI bypass.

## Real backend integration

`frontend-security-integration.cjs` copies the current Go package into a unique scratch directory, installs the test fixture there, and runs its isolated server test. The fixture uses the real CSRF/authentication/session/account middleware and handlers, SQLite, and embedded templates. It does not call production `main`, manipulate OpenVPN/firewalls/PKI, or use production data. Only test credentials are present. It checks:

1. CSRF rejection before login; administrator forced password change; privileged/account actions blocked until changed; authenticated password update; session invalidation and new login.
2. User login; literal profile text; API password/seed redaction; new MFA enrollment with generated OTP; session invalidation; session-bound MFA second stage; no active seed/QR display; CSRF-protected POST logout.

This is not a substitute for the full application's Docker/OpenVPN smoke test. The fixture does not register main's service, settings, import/export, PKI, or firewall handlers. Those UI rendering/request paths are browser-tested using explicit API fixtures, while their server implementations need the backend suite/container validation. No full deployment, LDAP server, email delivery, or VPN dataplane validation is claimed here.

## Sink review

Reviewed every first-party JS file and inline template script (vendor libraries unchanged). Remaining HTML builders contain static layout plus explicit HTML text/quoted-attribute encoding; the helper must never be used for JavaScript, CSS, or URL validation. URL query/path data uses URL APIs/encoding; downloads require same-origin HTTP(S). Messages and MFA seeds use `.text`, file names use `textContent`, editable LDAP/settings values use `.val`, and selected group matching uses attribute APIs. DataTables' plain string fields have explicit text renderers. Static empty `innerHTML` resets do not consume untrusted data. Remaining dynamic classes/icons are fixed internal choices; popover values are escaped again after attribute decoding.

`firewall.js` preserves its original CRLF line endings. Whitespace validation:

```sh
git -c core.whitespace=cr-at-eol diff --check -- src/openvpn-web/templates tests
```
