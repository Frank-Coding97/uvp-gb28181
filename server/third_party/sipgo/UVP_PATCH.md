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

## Originate from the pinned local port

Upstream v1.4.0 lets a client pin its local socket through
`Client.connAddr` → `Request.Laddr`, but the lookup is a **string match** on the
pooled connection address (`TransportLayer.ClientRequestConnection`). A datagram
listener is normally bound to a wildcard address, so its pool key is
`"[::]:5060"` while the pinned address is `"192.168.10.220:5060"`: the match
always fails, the transport dials a brand new socket from an ephemeral port, and
the request leaves from a port that the caller never advertised.

UVP adds a port-level fallback: when `laddr.IP != nil && laddr.Port > 0` and no
connection matches the address, the transport layer asks the transport for the
pooled **serving socket** bound on that port
(`listenerConnectionByPort`, implemented by `TransportUDP` as
`GetListenerConnection`). That socket is the only one able to originate from the
pinned port, and responses arriving on it are already dispatched to the
transaction layer by branch, so the client transaction completes normally.

Motivating case: GB28181 cascade registration. The downstream platform writes its
own SIP port into `Via`/`Contact`; a strict upstream (WVP) correlates every
message with the address the registration came from and **silently drops** a
`MESSAGE` whose source port differs — the registration looks healthy while the
catalog never lands (0 channels, no error in the upstream log). Loose upstreams
(LiveGBS, EasyGBS) accept it, which is why the defect survived until cascade
acceptance.

Scope: UDP only. Reliable transports (TCP/TLS/WS) have no serving socket that can
originate, so their behaviour is unchanged — a fresh connection from an ephemeral
port is the correct and only option there.

Regression guards: `sip/transport_udp_listener_port_test.go` (pool lookup and
reference counting) and, in the app,
`app/gb28181/sip/cascade_client_source_port_test.go` +
`app/gb28181/sip/cascade_register_digest_test.go` (wire-level source port and Via
for both the first REGISTER and the digest-challenge retry).

## Sent-by comes from the pinned local address

`clientRequestCreateVia` used `Client.host`/`Client.port`, which are empty unless
the caller opted into `WithClientHostname`/`WithClientPort`. The transport layer
then filled the gap from the connection's local address — `"[::]:52064"` for a
shared wildcard-bound listener. That yielded `Via: SIP/2.0/UDP [::]:52064`, which
EasyGBS answers with `400 Bad Request` **without any Via header**; the client
transaction can never match that response, so it retransmits until timeout while
the platform reports nothing useful.

The generator now prefers `Request.Laddr` when the request pinned a usable local
address (that address is what the socket will actually originate from), and only
then falls back to `Client.host`/`Client.port` and the transport fill-in.

Motivating case: the cascade REGISTER digest-challenge retry. sipgo rebuilds the
Via for the retry, so a caller that pinned `Laddr` for the first attempt silently
lost both the pinned port and a valid sent-by on the second one.

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

## Exported `DialogRequestRecipient`

Upstream only applies the "peer echoed our own Contact" correction inside
`newAckRequestUAC` / `newByeRequestUAC`. UVP has to build MANSRTSP `INFO`
requests by hand (playback TEARDOWN / pause / seek), and it was doing so from
`InviteResponse.Contact()` alone — which reintroduced exactly the bug those two
helpers were patched to avoid.

`DialogRequestRecipient(inviteRequest, inviteResponse) *sip.Uri` exposes the
same decision:

- no `inviteResponse` / no Contact → the INVITE recipient;
- Contact whose user part equals our own Contact user part (the echo case,
  measured against WVP-Pro v2.7.4 acting as a lower-level cascade platform,
  which answers `Contact: <sip:34020000012000000001@<our address>:<our port>>`)
  → the INVITE target (`inviteRequest.Recipient` with host/port taken from
  `inviteRequest.Destination()`);
- otherwise the Contact URI, per RFC 3261 §12.1.1.

Why it matters: the echoed Contact is an **address literal**, so the
`WithClientFallbackToResponseSource` fallback above deliberately does not fire
(it only applies to non-literal hosts). The `INFO` therefore left for our own
listening socket, our own stack answered `481 Dialog Does Not Exist`, and every
playback stop reported `playback control failed` (HTTP 502) while the peer kept
a dead dialog.

Pinned by `app/gb28181/uac/playback_recipient_echo_test.go`
(`TestDialogRequestRecipientRejectsEchoedLocalContact`,
`TestDialogRequestRecipientKeepsWellFormedContact`,
`TestDialogRequestRecipientFallsBackToInviteTarget`).

