# Proposal

macOS USB discovery creates a `CommandBackend` for the raw AT bulk transport. The backend currently lacks `SIMAuthProvider`, so the business adapter omits `apdu` and VoWiFi cannot start. Add the AT logical-channel SIM authentication methods and share the device APDU arbiter with the eSIM AT port.

## Goals

- Report `CapabilityAPDU` when the macOS USB AT transport is available.
- Implement USIM/ISIM logical channel open, APDU transmit, and close with bounded response parsing.
- Serialize VoWiFi SIM authentication and eSIM APDU work through one device arbiter.

## Non-goals

- Add QMI or MBIM behavior.
- Change the VoWiFi protocol or HTTP schema.
- Claim support for modules that reject AT+CCHO or AT+CGLA at runtime.
