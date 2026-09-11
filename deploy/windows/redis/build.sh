#!/usr/bin/env bash
# Run with the isolated Cygwin bash. Outputs never go into pristine source clones.
set -euo pipefail
export PATH=/usr/bin:/bin
export LC_ALL=C.UTF-8
root=${1:?usage: build.sh /cygdrive/g/uvp-p0-redis}
cd "$root"
printf '%s  %s\n' 2b268a157e99409b5326d8e8c7c91618ff50757a64f430a75477645fb775fbb8 redis-335554f1.tar.gz | sha256sum -c -
if [[ -e source || -e package ]]; then
  echo 'Refusing to overwrite a previous build' >&2
  exit 1
fi
mkdir source package
tar -xzf redis-335554f1.tar.gz -C source
uname -a > toolchain.txt
gcc --version >> toolchain.txt
make --version >> toolchain.txt
cygcheck -c >> toolchain.txt
# Keep installed headers and the Redis source unchanged. Request GNU declarations
# through the compiler instead of rewriting Cygwin's dlfcn.h.
make -C source -j4 BUILD_TLS=yes MALLOC=libc OPTIMIZATION=-O2 CFLAGS='-D_GNU_SOURCE -Wno-char-subscripts' redis-server redis-cli
cp source/src/redis-server.exe source/src/redis-cli.exe package/
cp source/COPYING package/REDIS-COPYING
for dll in cygwin1.dll cygcrypto-3.dll cygssl-3.dll cygz.dll; do
  cp "/usr/bin/$dll" package/
done
./package/redis-server.exe --version > redis-version.txt
cygcheck ./package/redis-server.exe > dependencies.txt
sha256sum package/* > package-sha256.txt
echo 'Build only: native command, persistence and distribution checks remain required.'
