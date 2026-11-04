FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

ARG TARGETPLATFORM

COPY --chmod=0555 ${TARGETPLATFORM}/vsfleet /usr/bin/vsfleet

# The image upgrades by pulling a new tag, so it never checks for releases.
ENV VSFLEET_NO_UPDATE_NOTIFIER=1

USER 65532:65532
ENTRYPOINT ["/usr/bin/vsfleet"]
