# multi-stage image build enables us to leave behind all the build bits once we have our binary
FROM openshift/origin-release:golang-1.10 AS build
ENV OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator
COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}
RUN yum update -y --exclude=golang && yum clean all && make gobuild

# Run the operator from the RHEL Universal Base Image
FROM registry.access.redhat.com/ubi7-dev-preview/ubi-minimal:7.6
ENV OPERATOR_BIN=configure-alertmanager-operator \
    USER_UID=1001 \
    USER_NAME=configure-alertmanager-operator \
    OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator

WORKDIR /root/
COPY --from=build ${OPERATOR_PATH}/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
USER ${USER_UID}
LABEL io.openshift.managed.name="configure-alertmanager-operator" \
      io.openshift.managed.description="Operator to configure Alertmanager with PagerDuty and Dead Man's Snitch."

COPY build/bin /usr/local/bin

ENTRYPOINT ["/usr/local/bin/configure-alertmanager-operator"]

USER ${USER_UID}

