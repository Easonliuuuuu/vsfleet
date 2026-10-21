FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88

ARG TARGETPLATFORM

COPY --chmod=0555 ${TARGETPLATFORM}/vsfleet /usr/bin/vsfleet

# The image upgrades by pulling a new tag, so it never checks for releases.
ENV VSFLEET_NO_UPDATE_NOTIFIER=1

USER 65532:65532
ENTRYPOINT ["/usr/bin/vsfleet"]
