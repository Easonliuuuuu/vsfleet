# Security Policy

vsfleet is designed to operate in enterprise environments spanning multiple
security zones and networks. Security, privacy, and operational safety are core
design priorities.

---

## Security Guarantees & Architecture

### 1. Strictly Read-Only Operations
vsfleet does not contain functionality to modify infrastructure. It cannot:
* Power VMs on, off, or reset them.
* Create, revert, or delete snapshots.
* Reconfigure virtual machines, networks, or datastores.
* Provision or delete inventory objects.

This invariant is enforced at the API layer in `internal/vsphere`: only query
and read-only information collection APIs are implemented.

### 2. Credential Security & Zero Disk Storage
* **No Plaintext Passwords:** vsfleet never writes passwords to `config.toml`,
  log files, or command history.
* **OS Keyring Integration:** When `credential = "keyring:<name>"` is configured,
  passwords are stored in the operating system's native secret manager:
  * Linux: Secret Service API (via `libsecret` / DBus)
  * macOS: Keychain Services
  * Windows: Credential Manager
* **Volatile Memory:** Passwords stored in memory are dereferenced as soon as
  sessions terminate.
* **Headless Safety:** On systems without a secret store (e.g., locked-down
  containers or headless minimal Linux), vsfleet safely falls back to prompt
  mode (`prompt`) rather than failing or storing credentials insecurely.

### 3. Network Isolation & Transport Security
* **Per-Context Isolation:** Each configured vCenter maintains its own isolated
  transport dialer and TLS policy. A compromised or misbehaving proxy route for
  one context cannot intercept traffic meant for another.
* **Certificate Thumbprint Pinning:** When internal enterprise vCenters use
  self-signed certificates or private enterprise CAs not present in the system
  trust store, vsfleet supports pinning SHA-256 or SHA-1 certificate fingerprints
  (`--tls thumbprint`). Any future change in the presented certificate halts the
  connection immediately to prevent man-in-the-middle (MITM) attacks.

---

## Reporting a Vulnerability

If you discover a security vulnerability in vsfleet, please **do not** report it
via a public GitHub issue.

Instead, please report it through one of the following channels:
1. **GitHub Security Advisory:** Open a private draft advisory via GitHub's
   Security tab on the repository.
2. **Email:** Send details directly to the project maintainers.

Please include:
* Description of the vulnerability and its potential impact.
* Steps to reproduce or proof-of-concept configuration.
* Any potential remediations or patches you have identified.

### Response Timeline
* **Initial Acknowledgement:** Within 48 hours of receipt.
* **Assessment & Fix:** We aim to investigate and develop a patch within 7 business days.
* **Public Disclosure:** Coordinated after a patched release is published.
