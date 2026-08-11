# Device Control

The Device Control view reports normal USB, Android Debug Bridge (ADB), and Emergency Download (EDL) state.

## EDL Information

DJOneHub reads EDL information from the active Qualcomm Sahara endpoint. It does not use a cached normal-mode firmware revision as current EDL information.

The view can show these values when the device provides them:

- Sahara or Firehose state
- Protocol source and observation time
- Masked Sahara serial number
- Masked hardware identifier (HWID)
- Masked public key hash
- Secondary Boot Loader (SBL) version
- Recovery reason

Sahara does not provide the modem firmware revision that the AT interface reports. DJOneHub leaves that revision empty in EDL and shows a separate reason.

## Browser Control Lease

The server owns one control session for one physical device. A browser tab acquires a renewable lease before it changes device state. Another tab can read status, but it cannot start a conflicting device operation.

The browser stores the lease token in tab-local session storage. A second tab does not share the token.

## NAND Backup And Reset

A successful NAND backup leaves the device in EDL. The backup does not reset the device.

Use **Restore normal mode** as a separate action. This action sends the Firehose reset request and waits for a supported normal USB identity at the same physical location.

DJOneHub can attempt one bounded cleanup reset after a failed or cancelled NAND read. The operation error reports whether manual recovery is required.

EDL entry and reset change device state. Do not run these actions on a real device without authorization. NAND read is read-only, but the device must already be in EDL.
