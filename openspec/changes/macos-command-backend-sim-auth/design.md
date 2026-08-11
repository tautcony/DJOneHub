# Design

`CommandBackend` gains an optional `*apduarbiter.Arbiter` and `SetAPDUArbiter`. Each SIMAuth operation acquires a transport lease with class `USIMAKA`, owner `sim_aka`, and the logical channel when known. The lease is released after the single AT command and is touched before and after the command. Without an arbiter, the backend still performs the command for compatibility.

The backend uses the existing AT command transport and mirrors the tested AT command forms:

- `AT+CCHO="<uppercase AID>"`, parse a channel in `+CCHO:` and require 1..255.
- `AT+CGLA=<channel>,<hex length>,"<uppercase APDU>"`, parse quoted hexadecimal data in `+CGLA:`.
- `AT+CCHC=<channel>`.

`darwin.Adapter.OpenAT` passes its newly created arbiter to the CommandBackend before constructing the eSIM AT port. Capability reporting adds `apdu` because the backend now implements `SIMAuthProvider`; no platform capability is added independently.

The VoWiFi host uses a narrow backend interface instead of requiring the legacy `DeviceBackend`. `ModemBackend` and `DeviceBackend` have incompatible SMS method signatures, so a direct CommandBackend cannot implement both. The narrow interface contains only identity, serving-system, operating-mode, and SIM authentication methods. CommandBackend implements these methods over the existing USB AT transport.
