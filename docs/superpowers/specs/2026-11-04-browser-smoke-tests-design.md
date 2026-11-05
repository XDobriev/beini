# Browser smoke tests

- **Intent:** make browser checks reproducible from this repository instead of relying on global npm packages or a particular browser app exposed to Codex.
- **Goals:** add a pinned Playwright test dependency and lockfile; exercise the guest menu/cart in desktop Chromium and mobile Chromium; run only against the loopback mock with ordering closed; run the same browser suite in CI.
- **Non-goals:** frontend build tooling, runtime dependencies, live iiko/production tests, and a broad cross-browser matrix.
- **Approaches:** manual CUA checks need no dependency but are not repeatable; global Playwright is already ambiguous across runtimes; repository-local Playwright with its managed Chromium is deterministic and fits the existing mock server. Use the repository-local approach.
- **Constraints:** preserve HTML/CSS/Vanilla JS; keep `python scripts/check.py` offline and independent; never submit a real order from browser tests.
- **Implementation calls:** pin `@playwright/test` to the version already bundled with Codex (1.62.1), install its Chromium in CI, start `scripts/mock-ui.py` on port 8766 in closed mode, and cover menu/variant/cart behavior at desktop and mobile viewports.
