# Disposable Windows Sandbox test runner

This helper is test infrastructure, not the product launcher. It needs a WSB
configuration mapping a dedicated host input directory read-only to
`C:\uvp-input`, and a dedicated evidence directory writable to `C:\uvp-output`.
Disable networking and vGPU. The logon command runs `runner.ps1` with the
built-in Windows PowerShell. Do not map user documents or business data.

Submit a uniquely named `name.ps1`, then create `name.request` in the input
directory. Publish the script completely before the request. Names allow only
ASCII letters, digits and hyphens. The runner writes `name.stdout`, `name.stderr`
and `name.exit`; consume the result only after `.exit` exists. A runner failure
also produces `.runner-error`. Use a fresh evidence directory for a new runner
session; existing result names are deliberately rejected rather than replaced.

Never rewrite an input file already opened by Sandbox: the read-only mapping
can retain host file locks. Each request is a new file. A `runner-stop.txt`
marker ends the polling loop; it does not stop the Sandbox itself.

Start the WSB in the logged-in user's interactive session. SSH session 0 is
not that desktop. On the observed Win10 host, `schtasks /IT` with the fully
qualified account worked; `New-ScheduledTaskPrincipal` registration did not.
Temporary test tasks must be removed once dispatched, and must not become
product service/startup entries. If restarting Sandbox, wait until its owned
VM has stopped before creating another instance. A start can time out: stale
evidence is not a new successful run.

Native validation on Windows 10 build 19041 included an intentional `exit 42`
script, a passing environment probe (`exit 0`), Redis version execution and
the SQLite native probe. No developer tools are required inside Sandbox.
