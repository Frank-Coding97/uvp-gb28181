# Redis Windows P0 build

This is a project-maintained Cygwin build of Redis, not an official Windows
distribution. It is a P0 candidate, not the UVP standalone package.

The input archive is produced by `git archive --format=tar.gz` from the pristine
Redis commit `335554f18caf7bbf6b0ac2b3548133d750f00a1b` (7.2.16). The script checks
its exact SHA-256 before extraction and refuses to overwrite a prior build.
The archive has no Git directory, so Redis reports `sha=00000000`; the source
commit is tracked here and in `../sources.lock.json`, not inferred from that
runtime field.

On the build host, use a dedicated Cygwin root and package cache. The observed
toolchain is Cygwin 3.6.10-1, GCC 14.4.0-1, Make 4.4.1-2, OpenSSL/libssl3
3.5.8-1 and zlib0 1.3.2-1. Preserve `toolchain.txt`, the signed setup metadata,
downloaded packages and source packages. A future mirror's defaults are not a
reproducible replacement for those inputs.

The Cygwin setup options used for the isolated build root were `-q -B -n
--no-write-registry -I`, with explicit `-R` root, `-l` package cache and `-s`
mirror, plus `make,gcc-core,gcc-g++,pkg-config,libssl-devel,zip`. This creates no
service, shortcuts or global PATH entry. The installer is separate from the
runtime package. Never use `--no-verify`.

Run with the isolated Cygwin bash:

```sh
bash --noprofile --norc build.sh /cygdrive/g/uvp-p0-redis
```

The build uses libc allocation, TLS and `-O2`, without changing Redis source or
Cygwin headers. GNU declarations are requested through `-D_GNU_SOURCE`.
The four vendored DLLs were confirmed by `cygcheck` and by running `--version`
from a Chinese/space path inside an offline Windows 10 Sandbox without Cygwin.
That smoke test does not establish AOF durability or all command compatibility.

Redis COPYING, Cygwin/Newlib licenses, OpenSSL and zlib licenses, their exact
corresponding sources and packaging patches must accompany a distributable
candidate. Keep the Cygwin DLL replaceable. The P0 build alone does not close
the distribution checklist or T02-C.

The optional native disk-full probe requires a disposable local volume labeled
`UVP_P0_TEST` no larger than 128 MiB. `prepare-disk-full.ps1` creates a new
64 MiB VHD and refuses existing paths or occupied drive letters. Windows
Sandbox may lack the virtual-disk provider; in that case use the isolated
build host for this fault injection and retain separate Sandbox command/AOF
evidence. Do not substitute a normal system or data drive.

After the probe has stopped its own Redis process, detach only the test VHD
using DiskPart `select vdisk file="<exact test VHD path>"` and `detach vdisk`.
The VHD file may be retained. Never select a physical disk by number for this
procedure. This helper is test tooling and is not part of product startup.
