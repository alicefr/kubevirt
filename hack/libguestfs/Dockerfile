FROM fedora:32 AS builder

env LIBGUESTFS_BACKEND direct

RUN dnf update -y --setopt tsflags=nodocs  && dnf install --setopt=install_weak_deps=False --setopt tsflags=nodocs -y \
    libguestfs  \
    libguestfs-devel \
    && dnf clean all

RUN mkdir -p /output \
    && libguestfs-make-fixed-appliance /output

FROM scratch 

COPY --from=builder /output/* /appliance/
