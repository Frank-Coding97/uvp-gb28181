# UVP sipgo compatibility patch

This directory is based on `github.com/emiago/sipgo v1.4.0`.

UVP adds `sip.TransportWriteObserver` and
`sip.WithTransportLayerWriteObserver` for UDP, TCP, and TLS transports, plus
a connection-close observer for TCP and TLS. The write observer receives a
copy of the final serialized SIP message immediately
before the socket write. Observer mutation or panic cannot alter the sent
bytes or transport result.

The close observer lets UVP discard partial per-connection TCP frame buffers
as soon as sipgo stops reading that connection.

The compatibility contract is covered by
`app/gb28181/sip/write_observer_test.go`, which compares observer bytes with
real UDP and TCP sockets for requests and server responses. Upgrade this fork
only after those byte-equality tests pass against the new upstream version.

The upstream module archive does not include the TLS certificate fixtures
referenced by its root integration tests. `go test ./sip` is the available
upstream transport regression suite for this pinned source archive.
