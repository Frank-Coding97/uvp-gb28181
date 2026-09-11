# SQLite runtime probe

This standalone module verifies the SQLite runtime candidate for the Windows
package without importing the server module. It uses `modernc.org/sqlite`
v1.58.0, whose upstream package documentation lists SQLite 3.53.4 for
Windows/amd64. The build is required to use `CGO_ENABLED=0`.

Run the tests and a local probe from this directory:

```sh
go test ./...
go vet ./...
go run .
```

Build the Windows x64 executable on any Go 1.25 host:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o sqlite-probe.exe .
```

The default probe creates and removes a temporary database under a path that
contains Chinese characters and spaces. Its JSON report includes the runtime
SQLite version/source ID, effective `journal_mode`, `synchronous`,
`foreign_keys`, `busy_timeout`, single-connection pool settings, each test
case, and an exit code. `-path` can be used for a dedicated existing test
directory on a target machine.

The probe is evidence for the runtime and transaction contract only. It does
not connect the server to SQLite, generate the business baseline, or qualify a
Windows release package.
