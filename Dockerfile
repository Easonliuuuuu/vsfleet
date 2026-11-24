FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931

ARG TARGETPLATFORM

COPY --chmod=0555 ${TARGETPLATFORM}/vsfleet /usr/bin/vsfleet

# The image upgrades by pulling a new tag, so it never checks for releases.
ENV VSFLEET_NO_UPDATE_NOTIFIER=1

USER 65532:65532
ENTRYPOINT ["/usr/bin/vsfleet"]
