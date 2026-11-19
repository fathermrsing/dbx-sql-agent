# Privacy and data access

This plugin has no analytics, telemetry, advertising or remote update service.

- It reads SQL Server connection summaries from DBX. It does not read database passwords from DBX.
- It calls the user-configured DBX MCP HTTP endpoint on the local loopback interface only. Redirects and non-loopback endpoints are rejected. DBX connects to the selected SQL Server using DBX-managed connection credentials.
- It queries SQL Server instance identity, roles, Agent job definitions, step commands, schedules and history. SQL previews and exports can contain sensitive job commands. Review them before sharing.
- Management changes are submitted only after a preview and explicit target confirmation. Reads and previews do not perform management writes. A submitted start request can cause existing job code to run on the server.
- An explicitly remembered MCP Bearer Token is stored through macOS Keychain Services under the plugin's own fixed service and a hash of the normalized MCP endpoint. The plugin does not enumerate unrelated keychain items. The UI can check whether a token exists but cannot retrieve the saved token. The backend loads it when needed. System access approval may be required.
- Other builds do not silently fall back to plaintext credential storage. Manually entered credentials are kept in process/UI memory. An optional user-provided environment variable can supply the MCP token.
- DBX plugin storage contains the selected endpoint/connection and an operation recovery ID, not the Bearer Token.
- The DBX-assigned plugin data directory contains operation status files and a mutation lock. These records can contain connection IDs and job names; they do not intentionally contain tokens or full SQL statements. Database error messages and rendered results may disclose schema information.
- The plugin does not open listening ports or launch shells/external commands during normal operation. Its Go native backend runs with the current user's privileges; it is not an OS security sandbox.

Use the **Clear saved Token** button to delete this plugin's saved credential for the current MCP endpoint. This does not revoke the server-side token, delete tokens for other endpoints, or remove a user-configured environment variable. Uninstalling a plugin may not remove its keychain item; clear it first if desired.

Only submit redacted logs to the public issue tracker. Never include tokens, database passwords, production job commands or proprietary database output.
