FROM jlesage/chromium@sha256:b5eaf481e22539d82c7c608a33df3598fb11652cb17d8491751c14dd149b272f

# The inherited nginx file capability requests NET_BIND_SERVICE, but this
# worker serves an unprivileged loopback port with NNP and a bounded cap set.
# A plain copy retains the exact binary bytes without inherited file xattrs.
RUN apk add --no-cache font-wqy-zenhei \
    && cp /opt/base/sbin/nginx /opt/base/sbin/nginx.unprivileged \
    && cmp /opt/base/sbin/nginx /opt/base/sbin/nginx.unprivileged \
    && chmod 0755 /opt/base/sbin/nginx.unprivileged \
    && mv /opt/base/sbin/nginx.unprivileged /opt/base/sbin/nginx

LABEL org.opencontainers.image.title="ClawManager Browser Worker" \
      org.opencontainers.image.description="Visible Chromium worker with persistent profile and CDP control for OpenClaw Lite" \
      org.opencontainers.image.version="0.2.0"
