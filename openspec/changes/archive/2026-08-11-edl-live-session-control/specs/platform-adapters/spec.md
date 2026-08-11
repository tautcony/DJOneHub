## ADDED Requirements

### Requirement: Platform adapters SHALL expose verified Sahara observation

An adapter SHALL expose Sahara observation only after validating the EDL USB identity, endpoint selection, protocol handshake, response bounds, and cancellation. Unsupported platforms SHALL omit the capability and return a structured unsupported error.

The macOS adapter SHALL accept USB identity `05c6:9008` and bulk endpoints on interface 0 only. It SHALL bound protocol packets to 4096 bytes and execute values to 1024 bytes. Linux and Windows SHALL return `capability_not_supported` until their Sahara transports are verified.

The Sahara observer SHALL accept a HELLO request, an existing `CMD_READY` state, or a Firehose XML response as its first bounded packet. It SHALL NOT require a new HELLO when the device is already in Sahara command mode.

#### Scenario: Sahara response is malformed
- **WHEN** the endpoint returns an oversized or malformed Sahara packet
- **THEN** the adapter returns a structured protocol error and no observed identity facts
