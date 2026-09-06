FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

ARG TARGETPLATFORM

COPY --chmod=0555 ${TARGETPLATFORM}/vsfleet /usr/bin/vsfleet

USER 65532:65532
ENTRYPOINT ["/usr/bin/vsfleet"]
