# miniz 3.0.2 (vendored)

Single-file DEFLATE/zlib implementation, MIT-licensed (see LICENSE).

- Source: https://github.com/richgel999/miniz/releases/download/3.0.2/miniz-3.0.2.zip
- miniz.c sha256: 0fcdc9888cb3a29ca8f176bac087e5fe6c7258a6ab06b1c271c1e109a11d3740

Consumed only by `src/util/inflate.c` (unity include, warnings suppressed
locally), which layers gzip (RFC 1952) header/trailer handling — including
CRC32 verification via `mz_crc32` — over miniz's raw-DEFLATE decoder.
Archive/stdio/time features are compiled out; the linker drops the unused
compressor.

To upgrade: replace miniz.c/miniz.h/LICENSE with a newer release, update the
pin above, and run `make test` — the gzip fixtures exercise the integration.
