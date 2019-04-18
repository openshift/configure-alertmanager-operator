FROM registry.svc.ci.openshift.org/openshift/release:golang-1.11 AS builder
ENV OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator
COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}
RUN make go-build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
ENV OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator \
￼     OPERATOR_BIN=configure-alertmanager-operator

WORKDIR /root/
COPY --from=builder ${OPERATOR_PATH}/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
USER ${USER_UID}
LABEL io.openshift.managed.name="configure-alertmanager-operator" \
      io.openshift.managed.description="Operator to configure Alertmanager"
