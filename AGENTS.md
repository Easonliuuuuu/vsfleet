# Repository guidance

Use the checked-in testbed interface for synthetic UI work:

```sh
scripts/testbed list
scripts/testbed test
scripts/testbed sandbox overview
```

Read [docs/testbed.md](docs/testbed.md) for profile boundaries and
[docs/testing.md](docs/testing.md) for the complete verification ladder. Keep
presentation fixtures deterministic, offline, read-only, and visibly
synthetic. Connected fixtures must remain loopback-only and isolated from
operator configuration, credentials, keyrings, and network state.
