## govulncheck-source.json: FAILED

govulncheck v1.9.0, source scan, go1.40.1. Database built 2030-01-10T00:00:00Z (limit 336h0m0s).

### Gate failures

- exception CVE-2030-2002 in tzdata: expired on 2030-01-01

### Blocking findings

| Advisory | Artifact | Version | Fixed in | Severity | Why |
| --- | --- | --- | --- | --- | --- |
| GO-2030-0004 | stdlib | v1.40.1 | v1.40.2 | - | net/http.Client.Do is reachable |

### Accepted by exception

| Advisory | Artifact | Version | Owner | Expires |
| --- | --- | --- | --- | --- |
| GO-2030-0001 | example.test/parser | v1.2.3 | maintainer | 2030-02-01 |

### Reported, not blocking

| Advisory | Artifact | Version | Fixed in | Severity | Why |
| --- | --- | --- | --- | --- | --- |
| GO-2030-0003 | example.test/crypto | v0.5.0 | none | - | module required, vulnerable package not imported |
| GO-2030-0002 | example.test/net | v0.8.0 | v0.9.0 | - | package imported, vulnerable code not called |
