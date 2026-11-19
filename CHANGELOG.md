# Changelog

## 0.1.3 — Initial store review candidate

- Prepare the SQL Server Agent Manager source for public review and add source/homepage metadata.
- Anonymize legacy-response fixtures; no production connection identifiers or login names are needed to reproduce the tests.
- Document permissions, local MCP network access, Keychain behavior and remaining integration-test limitations.
- Preserve the 0.1.2 features: Agent jobs, T-SQL steps, schedules, history, reviewed management operations, legacy DBX response compatibility and opt-in macOS Keychain token storage.

Supported release target: **macOS Apple Silicon (`darwin-arm64`) only**.

This is a development preview submitted for review, not a claim of DBX Store approval. Automated tests use mock MCP and an in-memory credential vault. Real DBX installation, native Keychain consent/restart behavior and real SQL Server management operations still require integration validation. No real database or user keychain has been accessed by the tests.
