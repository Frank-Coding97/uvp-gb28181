#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/bin:/bin
cd "${1:?usage: collect-sources.sh /cygdrive/g/uvp-p0-redis}"
mkdir -p redistribution-sources licenses
cp redis-335554f1.tar.gz redistribution-sources/
cp -r /usr/src/cygwin-3.6.10-1.src /usr/src/openssl-3.5.8-1.src /usr/src/zlib-1.3.2-1.src redistribution-sources/
cp source/COPYING licenses/REDIS-COPYING
cp source/deps/hiredis/COPYING licenses/HIREDIS-COPYING
cp source/deps/lua/COPYRIGHT licenses/LUA-COPYRIGHT
cp source/deps/hdr_histogram/LICENSE.txt licenses/HDR-HISTOGRAM-LICENSE
cp source/deps/hdr_histogram/COPYING.txt licenses/HDR-HISTOGRAM-COPYING
cp source/deps/fpconv/LICENSE.txt licenses/FPCONV-LICENSE
# Linenoise carries its redistribution notice in the source file header.
cp source/deps/linenoise/linenoise.c licenses/LINENOISE-SOURCE-NOTICE.c
cp /usr/share/doc/openssl/LICENSE.txt licenses/OPENSSL-LICENSE
for name in COPYING COPYING.LIB COPYING.LIBGLOSS COPYING.NEWLIB COPYING3 COPYING3.LIB; do
  tar -xOf /usr/src/cygwin-3.6.10-1.src/newlib-cygwin-3.6.10.tar.bz2 "newlib-cygwin/$name" > "licenses/CYGWIN-$name"
done
tar -xOf /usr/src/cygwin-3.6.10-1.src/newlib-cygwin-3.6.10.tar.bz2 newlib-cygwin/winsup/CYGWIN_LICENSE > licenses/CYGWIN-LICENSE
tar -xOf /usr/src/zlib-1.3.2-1.src/zlib-1.3.2.tar.xz zlib-1.3.2/LICENSE > licenses/ZLIB-LICENSE
find redistribution-sources licenses -type f -exec sha256sum {} + > redistribution-sha256.txt
