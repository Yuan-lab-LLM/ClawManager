# Disposable local NFS fixture. Never use against a tenant workspace.
FROM node:24.21.0-bookworm-slim AS server
RUN apt-get update && apt-get install -y --no-install-recommends nfs-ganesha nfs-ganesha-vfs && rm -rf /var/lib/apt/lists/*
COPY ganesha.conf /etc/ganesha/ganesha.conf
COPY start-server.sh /usr/local/bin/start-fixture-nfs
RUN chmod 0755 /usr/local/bin/start-fixture-nfs
ENTRYPOINT ["/usr/local/bin/start-fixture-nfs"]

FROM openclaw:team-openclaw-10.2-recovery-20261013-v1 AS client
RUN apt-get update && apt-get install -y --no-install-recommends nfs-common strace && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/bin/sh"]
