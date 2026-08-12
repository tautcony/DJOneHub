## Modified Requirements

### Requirement: Backend construction SHALL use runtime discovery data

The backend factory and modem manager SHALL use a minimal runtime configuration derived from the discovered device candidate. They SHALL NOT import or read the legacy `internal/config` package or a YAML device configuration file.

#### Scenario: Normal device discovery

- **WHEN** the runtime opens a discovered AT-capable candidate
- **THEN** the factory maps the candidate to the modem runtime configuration and initializes the manager with the same port, backend mode, and device identity behavior as before

#### Scenario: Missing AT port for a pure protocol backend

- **WHEN** a QMI or MBIM manager is initialized without an AT port
- **THEN** the manager preserves the existing pure-backend startup behavior without requiring a legacy configuration file
