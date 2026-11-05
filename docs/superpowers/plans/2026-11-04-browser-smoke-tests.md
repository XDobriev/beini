# Browser smoke tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add reproducible desktop and mobile Playwright smoke tests against BEINI's offline mock.

**Architecture:** Keep the existing static frontend and Python mock server. A repository-local Playwright config starts the mock on loopback and runs Chromium projects at desktop and mobile sizes; CI installs the pinned test package and browser before running the suite.

**Tech Stack:** Node.js 22+, `@playwright/test` 1.62.1, Playwright-managed Chromium, existing Python mock server.

**Spec:** [browser smoke tests design](../specs/2026-11-04-browser-smoke-tests-design.md)

## Global Constraints

- Keep runtime UI libraries and frontend build tooling out of the project.
- Do not use production or iiko endpoints; mock runs in `closed` mode.
- Keep the existing offline `python scripts/check.py` usable without npm packages.
- Do not commit generated browser reports, test output, or `node_modules`.

## Review Focus

- Browser tests must not submit checkout; verify mock `creates` remains zero.
- CI must install the Playwright-matched Chromium on both supported runners.
- Tests must cover mobile cart entry and desktop/mobile variant selection.

## Tasks

### Task 1: Add repository-local browser test setup

- [x] Add one smoke test for menu and variant/cart selection, including a mock assertion that `creates` remains zero.
- [x] Add a mobile-only test opening and closing the cart sheet, without submitting checkout.
- [x] Add `@playwright/test` 1.62.1, `test:e2e` and browser install scripts, plus Chromium desktop/mobile projects and a webServer that starts the mock on port 8766 in `closed` mode.
- [x] Install the pinned package and Chromium, then run `npm run test:e2e`; 3 tests passed and the desktop-only skip is expected.

### Task 2: Make the setup repeatable for maintainers and CI

- [x] Ignore `node_modules`, `test-results`, and `playwright-report`.
- [x] Add `npm ci`, Playwright Chromium installation, and `npm run test:e2e` to the existing Windows/Linux CI matrix.
- [x] Update README and testing docs with setup and commands, clarifying that the Python checks remain npm-free.
- [x] Run `python scripts/check.py`; all checks passed.
