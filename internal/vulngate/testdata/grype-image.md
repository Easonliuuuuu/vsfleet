## grype-image.json: FAILED

grype 0.121.1, image registry.example.test/vsfleet@sha256:0000000000000000000000000000000000000000000000000000000000000000. Database built 2030-01-14T04:00:00Z (limit 336h0m0s).

### Gate failures

- exception CVE-2030-2002 in tzdata: expired on 2030-01-01

### Blocking findings

| Advisory | Artifact | Version | Fixed in | Severity | Why |
| --- | --- | --- | --- | --- | --- |
| GHSA-aaaa-bbbb-cccc | example.test/parser | v1.2.3 | v1.2.4 | High | fixable High |
| CVE-2030-2001 | libc6 | 2.36-9+deb13u8 | 2.36-9+deb13u9 | Critical | fixable Critical |

### Reported, not blocking

| Advisory | Artifact | Version | Fixed in | Severity | Why |
| --- | --- | --- | --- | --- | --- |
| CVE-2030-2003 | stdlib | go1.40.1 | v1.40.2 | Medium | Medium severity is below the gate |
| CVE-2030-2002 | tzdata | 2030a-1 | none | High | High, no fix (not-fixed) |
