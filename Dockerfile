FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

ARG TARGETPLATFORM

COPY --chmod=0555 ${TARGETPLATFORM}/vsfleet /usr/bin/vsfleet

# The image upgrades by pulling a new tag, so it never checks for releases.
ENV VSFLEET_NO_UPDATE_NOTIFIER=1

USER 65532:65532
ENTRYPOINT ["/usr/bin/vsfleet"]
