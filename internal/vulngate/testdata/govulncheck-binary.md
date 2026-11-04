## govulncheck-binary.json: FAILED

govulncheck v1.9.0, binary scan. Database built 2030-01-10T00:00:00Z (limit 336h0m0s).

### Gate failures

- exception CVE-2030-2002 in tzdata: expired on 2030-01-01

### Reported, not blocking

| Advisory | Artifact | Version | Fixed in | Severity | Why |
| --- | --- | --- | --- | --- | --- |
| GO-2030-0005 | example.test/crypto | v0.5.0 | none | - | example.test/crypto/legacy may be linked; a stripped binary cannot show it, so the source scan decides |
