FROM registry.access.redhat.com/ubi7-dev-preview/ubi-minimal:7.6

ENV OPERATOR=/usr/local/bin/configure-alertmanager-operator \
    USER_UID=1001 \
    USER_NAME=configure-alertmanager-operator

# install operator binary
COPY build/_output/bin/configure-alertmanager-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
