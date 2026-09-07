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

The runtime preflight rejects non-Windows-10-22H2/native-x64 hosts and detected
build tools or existing running components. Passing is **inventory only**:
software absent from PATH or not running can remain installed. T01-C still
requires a known clean OS image. P0 additionally requires actual native component
execution; the final package is tested later in T30. ARM64
Windows running x64 emulation does not satisfy the native-x64 test requirement.
Exit code 1 means blocked/failed, never a skip/pass. The probe has been executed through native Windows PowerShell 5.1 on Windows 10
22H2: build inventory passed, and runtime inventory correctly rejected an
existing development machine with running MySQL/Redis. This does not qualify
that machine as a clean runtime environment.
