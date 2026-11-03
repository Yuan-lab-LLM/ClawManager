ARG BASE_IMAGE=clawmanager-local/opencode-lite:2.0.27-20261012
FROM ${BASE_IMAGE}
ENV OPENCODE_RUNTIME_MAJOR=2
COPY rootfs/ /
RUN chmod 755 /usr/local/bin/opencode-pro-session.py /usr/local/bin/start-opencode-terminal /usr/local/bin/start-opencode-pro-web \
    && python3 --version \
    && bash -n /usr/local/bin/start-opencode-terminal /usr/local/bin/start-opencode-pro-web \
    && /usr/local/bin/opencode --version | grep -Fx 'opencode v2.0.27'
