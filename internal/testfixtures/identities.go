// Package testfixtures contains synthetic modem data for unit tests.
//
// These values are intentionally not copied from any physical device. Tests
// must use this package or locally generated data instead of live identities,
// SIM contents, SMS payloads, or profile identifiers.
package testfixtures

const (
	IMEI   = "350000000000006"
	IMEI14 = "35000000000000"
	IMEISV = "3500000000000001"

	IMSI    = "001010123456789"
	IMSIAlt = "001010987654321"

	ICCID19 = "8901000000000000000"
	ICCID20 = "89010000000000000001"

	MSISDN = "+15551234567"
	SMSC   = "+15551234567"

	EID    = "89010000000000000000000000000001"
	EIDAlt = "89010000000000000000000000000002"
)
