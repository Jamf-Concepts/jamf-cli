# Smoke Test Setup

Smoke tests verify all CLI endpoints against a real Jamf Pro instance. They catch field-mapping bugs, broken endpoints, and API drift that unit tests with mocked data cannot detect.

## Prerequisites

A configured CLI profile pointing at a Jamf Pro instance:

```bash
bin/jamf-cli config setup   # interactive profile creation
```

Or set environment variables: `JAMF_URL`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`.

## Commands

```bash
make smoke-seed       # Create minimal test data on the instance (one-time)
make smoke            # Run all smoke tests (Tier 1: endpoints + Tier 2: field paths)
make smoke-cleanup    # Remove all _smoke-test resources from the instance
make release-check    # Run unit tests + smoke tests (pre-release gate)
```

## What the smoke test covers

**Tier 1 — Endpoint reachability (259 GET endpoints):**
Every generated CLI command's GET endpoint is called. The test asserts:
- The endpoint returns HTTP 2xx
- The response is valid JSON
- For get-by-ID endpoints, the ID is discovered from the corresponding list endpoint

**Tier 2 — Field-path assertions (power commands):**
API responses used by `report`, `audit`, `overview`, and `bulk` commands are validated for specific field paths the code depends on (e.g., `hardware.serialNumber` in the inventory response, `general.remoteManagement.managed` for managed status, `operatingSystem.version` for OS version).

## Seeding test data

Many endpoints return empty lists on a fresh instance, which means their get-by-ID endpoints can't be tested. `make smoke-seed` creates one minimal resource per type, all prefixed `_smoke-test`:

- Classic API: saved searches, extension attributes, dock items, network segments, etc.
- Modern API: advanced searches

Run once after setting up a test instance. The seed is idempotent — it skips resources that already exist.

## Pre-release workflow

When a new Jamf Pro version drops:

```bash
make sync-specs JAMF_SERVER_PATH=/path/to/jss   # Pull new OpenAPI specs
make generate                                     # Regenerate CLI commands
make release-check                                # Unit tests + smoke tests
# If green: tag and release
```

## Skip reasons

Some endpoints will always skip:

| Reason | Meaning |
|--------|---------|
| 404 | Endpoint doesn't exist in this Jamf Pro version |
| 403 | API client lacks required privileges |
| 400 | Endpoint requires parameters that can't be inferred |
| 503 | Feature not provisioned on this instance |
| empty response | Endpoint returns 200 with no body |
| non-JSON (CSV) | Endpoint intentionally returns non-JSON |
| no ID available | List was empty — no ID to test get endpoint |

Resources that can't be seeded (require real hardware, Apple integrations, or server-side features): computer commands/history, VPP, DEP sync states, LAPS, GSX, DigiCert, Venafi, remote assist sessions, managed software updates.
