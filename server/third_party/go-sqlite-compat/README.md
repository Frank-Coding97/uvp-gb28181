# go-sqlite compatibility shim

This directory supplies the module path `github.com/glebarez/go-sqlite` for
the pinned `github.com/glebarez/sqlite v1.11.0` GORM dialect. It is an
intentional, minimal compatibility module rather than a copy or a fork of the
upstream driver.

## Supported surface

The only supported symbol is `sqlite.Error`, declared as an alias of
`modernc.org/sqlite.Error`. This is the complete surface required by the
pinned GORM dialect for error translation. `Driver`, `Open`, connection
helpers, and other upstream APIs are not provided. Code that needs SQLite
access should use `internal/sqlitedialect.Open`.

## Upstream correspondence

- GORM dialect: `github.com/glebarez/sqlite v1.11.0`.
- Compatibility module path: `github.com/glebarez/go-sqlite` (upstream
  reference `v1.21.2`, used only to identify the replaced module contract).
- Runtime driver: `modernc.org/sqlite v1.58.0`, which uniquely registers the
  `sqlite` database/sql driver and supplies the SQLite implementation.

The repository does not build the original `glebarez/go-sqlite` driver
implementation and this shim must not grow into an unverified reimplementation
of its API.

## License

The shim source is project-owned. The aliased runtime and the upstream
`glebarez/go-sqlite` reference are distributed under the BSD 3-Clause license;
the applicable license text is kept in [LICENSE](LICENSE). The retained GORM
dialect remains under its upstream MIT license.
