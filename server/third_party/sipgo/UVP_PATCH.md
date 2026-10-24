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

## Dialog request destination fallback

UVP adds `WithClientFallbackToResponseSource` on `Client`. It is **off by
default**, so a fresh `Client` behaves exactly like upstream v1.4.0; UVP turns
it on for the GB28181 UAC (`app/gb28181/uac`).

When the option is on and `DialogClientSession.buildReq` finds no Record-Route
in the response, an in-dialog request (ACK/BYE/INFO) **may have its transport
destination replaced by `InviteResponse.Source()`** — but only when the
Request-URI host is not an address literal (`netip.ParseAddr` fails). The
Request-URI itself is never touched.

Motivating case: a GB28181 device answers INVITE with the platform SIP domain
in Contact, e.g. `Contact: <sip:37010000001310000001@3701000000>`. RFC 3261
§13.2.2.4 requires the ACK Request-URI to stay that Contact URI, but `3701000000`
is not an IP literal and `_sip._udp.3701000000` SRV resolution always ends in
NXDOMAIN, so the ACK could never leave and the point-to-point play failed with a
rolled-back RTP port. Upstream `DialogServerSession.buildReq` carries the same
open TODO ("check is contact header routable ... force destination as source
address"); this option closes it for the client dialog only.

Invariants the tests pin down (`dialog_client_test.go`,
`TestDialogClientBuildReqUnroutableContactDestination`):

- default client: destination stays `3701000000:5060` (upstream behaviour);
- option on + unroutable Contact: destination becomes the response source, while
  Request-URI stays `sip:34020000001310000001@3701000000`;
- option on + Contact already an IP literal: destination unchanged;
- option on + response carries a Record-Route: the route set wins, no fallback;
- option on + empty `InviteResponse.Source()`: no guessing, destination unchanged.

Because the fallback only fires when the route set is empty, it does not perturb
UVP's frozen ACK/BYE material used for point teardown
(`validFixedDialogMaterial`, `setFixedDialogDestination`,
`playauth.DeviceSIPCleanupRequestIdentity`), which derive `Destination` from the
route set or the peer target.

The upstream module archive does not include the TLS certificate fixtures
referenced by its root integration tests. `go test ./sip` is the available
upstream transport regression suite for this pinned source archive.
