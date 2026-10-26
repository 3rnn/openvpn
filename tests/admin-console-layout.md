# Admin console UI

## Design contract

- Chinese workspace: a fixed slate sidebar on desktop; Bootstrap's existing offcanvas on mobile. Every management section is a direct native button, not an item hidden inside a management dropdown.
- White table/form surfaces, muted slate copy, restrained teal actions. No decorative or fabricated operational metrics; status data still comes from the original endpoint.
- Persistent section title, description, `aria-current`, keyboard-visible focus and skip link. Mobile navigation closes its focus trap **before** dispatching existing table/modal actions. Escape returns focus to the menu button.
- Original element IDs, API handlers, authentication, MFA, CSRF request wrapper and escaping remain intact. No new external assets or runtime dependencies.
- Login and user portal share the same locally served palette, surfaces and form treatment; login inputs have explicit labels.

## Browser verification

Install Playwright in an isolated tools directory and provide its `node_modules` via `NODE_PATH`. The application itself has no Node build step.

```sh
NODE_PATH=<tools>/node_modules GOTOOLCHAIN=go1.27.3 \
  node --test tests/admin-console-layout.cjs \
  tests/frontend-security.cjs tests/frontend-security-integration.cjs
```

`admin-console-layout.cjs` uses actual checked-out templates, CSS, vendor libraries and modules in headless Chromium. APIs use synthetic responses; three fixed Go-template presentation values are substituted for screenshots. The existing integration suite separately exercises actual Go template execution and auth/account handlers in a private loopback fixture.

Six UI cases cover direct desktop navigation, populated account layouts/details, companion portal presentation, padded account/settings forms, browser history/deep links, and mobile offcanvas/focus/modal reachability. Viewports include 320, 390, 768, 1024 and 1440 pixels. Body horizontal overflow is checked; wide server data can scroll inside its own panel and DataTables retains its responsive row disclosure.

Screenshots default to `/root/.hermes/cache/scratch/admin-console-screenshots`; override with `UI_SCREENSHOT_DIR`. Animations are disabled for screenshots to avoid capturing Bootstrap mid-transition. Account records and displayed status values in these images are explicitly synthetic, not production observations.

## Boundaries

This is UI and isolated browser/auth-fixture validation, not a full container/VPN acceptance test. No image build, deployment, production state, firewall, PKI or service operation is performed by the UI suite. Browser coverage is Chromium; Safari/Firefox, exhaustive assistive-technology audits and every legacy group/context-menu workflow are not claimed.
