# Windows P0 source and environment checks

These tools prepare T01. They do not build a Windows package or certify P0.
The task definitions and execution results live in Atlas.

## Source verification (build machine)

Requires Python 3.9+ and Git. Use independent, pristine source clones at the
commits in `sources.lock.json`; initialize ZLM submodules at the pinned gitlinks.
Build outputs must be outside those clones. The checker never downloads, resets,
cleans or checks out anything. Even ignored files fail validation because local
configuration or binaries must not silently become build inputs.

```sh
python3 deploy/windows/source_lock.py --lock deploy/windows/sources.lock.json \
  --uvp /path/to/pristine/uvp --zlm /path/to/pristine/zlm \
  --redis /path/to/pristine/redis
python3 deploy/windows/source_lock_test.py
```

The lock pins candidate **source inputs**, not a qualified toolchain or product.
Redis 7.2.16 is pinned to its official release commit; Cygwin/DLL versions and
redistribution material still need T02 qualification. Updating a component
requires updating its commit and complete recursive submodule list together.
The success JSON deliberately includes `windows_runtime_verified: false`.

## Environment inventory (native Windows)

Build the small standalone probe on a machine with Go 1.25+:

```sh
cd deploy/windows/environment-probe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o environment-probe.exe .
go test ./...
```

Copy only the resulting EXE to the Windows machines. No Go/Python installation
is needed on the runtime machine. It uses the built-in Windows PowerShell to
read OS/CPU metadata, build-tool names on PATH and relevant running process
names. It does not install software, start/stop processes or change services.

```powershell
.\environment-probe.exe -role build > build-environment.json
.\environment-probe.exe -role runtime > runtime-environment.json
```

The runtime preflight rejects Windows hosts below build 19041, Windows 11, non-native-x64 hosts and detected
build tools or existing running components. Passing is **inventory only**:
software absent from PATH or not running can remain installed. T01-C still
requires a known clean OS image. P0 additionally requires actual native component
execution; the final package is tested later in T30. ARM64
Windows running x64 emulation does not satisfy the native-x64 test requirement.
Exit code 1 means blocked/failed, never a skip/pass. The probe has been executed through native Windows PowerShell 5.1 on Windows 10
22H2: build inventory passed, and runtime inventory correctly rejected an
existing development machine with running MySQL/Redis. This does not qualify
that machine as a clean runtime environment.

## Standalone path verification

`server/cmd/standalone-path-probe/` is a small CGO-free Windows executable for T05. It
accepts the explicit `UVP_INSTALL_DIR`, `UVP_CONFIG_DIR`, `UVP_RESOURCE_DIR`,
`UVP_WEB_DIR`, `UVP_DATA_DIR`, and optional `UVP_RECORDINGS_DIR` inputs (the
same values may be passed as `-uvp-*-dir` arguments). The package root is
always supplied explicitly because the server binary lives below a versioned
release directory.

```sh
cd server
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o ../release-output/t05/standalone-path-probe.exe ./cmd/standalone-path-probe
```

Run the resulting executable from another working directory after setting the
six `UVP_*_DIR` variables above. It reports the current working directory and
the resolved config/data/web/recordings paths as JSON; it rejects relative,
UNC, escaped, missing, and non-writable roots without changing the working
directory or selecting a hidden fallback. Native Win10 execution is required
for T05 acceptance.

Example on the target machine (the `D:` recordings root is optional and may
be on another local drive):

```powershell
$env:UVP_INSTALL_DIR = 'C:\UVP-Windows'
$env:UVP_CONFIG_DIR = 'C:\UVP-Windows\config'
$env:UVP_RESOURCE_DIR = 'C:\UVP-Windows\resource'
$env:UVP_WEB_DIR = 'C:\UVP-Windows\web'
$env:UVP_DATA_DIR = 'C:\UVP-Windows\data'
$env:UVP_RECORDINGS_DIR = 'D:\UVP Recordings 中文'
Set-Location $env:TEMP
& "$env:UVP_INSTALL_DIR\standalone-path-probe.exe"
```

### SQLite runtime check

The standalone backend selects `gormv2.usedbtype: sqlite`; `data/uvp.db` is
fixed by the explicit data directory. Enabling another database at the same
time is rejected before a connection is opened. Outside standalone mode,
SQLite requires an absolute `gormv2.sqlite.path`.

`uvp-server.exe -db-check` opens the configured SQLite file, prints runtime
settings as JSON, then closes it. It may create a new empty database, but does
not initialize application tables, seed users, start HTTP/SIP, or register
scheduled jobs. SQLite is pinned to 3.53.4 with WAL, FULL synchronous mode,
foreign keys, a 5000 ms busy timeout, and one pooled connection. Ordinary write
transactions reserve the writer at entry (`_txlock=immediate`); explicitly
read-only transactions retain WAL read concurrency. Incremental migrations
and normal business startup remain gated until T08 is complete.

`test-db-check.ps1 -ServerExe <absolute-exe> -WorkRoot <new-directory>` checks
this entry point on Windows with a Chinese/space path, repeated open, unknown
dialect and conflicting MySQL configuration. Use an isolated temporary root.

### SQLite first initialization

`uvp-server.exe -bootstrap-db` applies the embedded, checksum-pinned SQLite
release baseline and civil-code dataset in one immediate transaction. It emits
`version`, `checksum`, and `created` as JSON and closes the database. Repeating
it verifies the existing baseline marker without replaying seeds or changing
user data. A non-empty database without the matching marker is rejected.
Schema, seeds, and marker roll back together on failure; an uncertain rollback
discards the connection. This command does not create an administrator or start
HTTP, SIP, media, Redis, or scheduled jobs.

`test-bootstrap-db.ps1 -ServerExe <absolute-exe> -WorkRoot <new-directory>`
checks first and repeated initialization using the actual backend on Windows,
including a Chinese/space/hash path and an unchanged database file checksum.
The Go initialization tests separately cover user/device/role preservation and
SQL, seed, cancellation, and marker failures.

When using Windows Sandbox, put the test `WorkRoot` on its internal disk
(for example `C:\uvp-local-tests\bootstrap-001`). Use WSB mapped folders only
for copying binaries and result logs. A mapped folder reported NTFS and a local
C: path by Win32 but faulted during SQLite WAL shared-memory access; the same
backend and tests passed on the Sandbox internal disk. WSB shared storage is
not a qualified SQLite data location.

### First-install integration fixtures

`prepare-installation-test.ps1` creates a new, isolated `t18-setup` release from
explicit launcher/backend binaries, Redis/media directories and web/resource
ZIP files. The web ZIP must contain `index.html` directly at its root and use
UTF-8 entry names. Existing destinations are rejected. This helper is for test
fixtures, not the final distribution builder.

Build the native test executable from `server`:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/standalone/launcher -o launcher-installation.test.exe
```

On Windows, set `UVP_T18_INSTALL_DIR` to a fresh fixture and run
`-test.run=TestWindowsStandaloneT18InstallationHTTPFlow`. Setting
`UVP_T18_SIP_IP` to a local non-loopback IPv4 address additionally exercises SIP
activation on port 15070 and a third restart preserving completed setup.

Use a different fresh fixture for `UVP_T18_BROWSER_INSTALL_DIR` and
`-test.run=TestWindowsStandaloneT18InstallationBrowserFlow`. It requires installed
Edge and uses a separate owned headless browser/profile. Credentials remain in
memory and CDP navigation; they are never test arguments or output. This does
not replace Explorer double-click or clean Windows Sandbox acceptance.
