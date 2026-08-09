## 1. Host-Compatible Fixtures

- [x] 1.1 Add an injectable proxy environment lookup and make precedence tests host-independent
- [x] 1.2 Create a Windows-resolvable EDL runner fixture
- [x] 1.3 Skip only Linux sysfs path-shape tests that Windows cannot represent

## 2. Connection And Logger Lifecycle

- [x] 2.1 Replace repeated WebSocket reads after timeout with one synchronized read
- [x] 2.2 Track initial rotation-handler completion and wait during logger shutdown
- [x] 2.3 Serialize Windows stable-log hard-link replacement

## 3. Verification

- [x] 3.1 Run affected package tests repeatedly on Windows
- [x] 3.2 Run the full Go test suite on Windows
- [x] 3.3 Validate the OpenSpec change strictly
