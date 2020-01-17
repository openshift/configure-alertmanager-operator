FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder

# Allow specifying a GOPROXY cache during build to speed up dependency resolution
ARG GOPROXY=https://proxy.golang.org

ENV OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator \
    GOPROXY=$GOPROXY

COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}
RUN make gobuild

####
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR_PATH=/go/src/github.com/openshift/configure-alertmanager-operator \
    OPERATOR_BIN=configure-alertmanager-operator

# install operator binary
COPY --from=builder ${OPERATOR_PATH}/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
LABEL io.openshift.managed.name="configure-alertmanager-operator" \
      io.openshift.managed.description="Operator to configure Alertmanager with PagerDuty and Dead Man's Snitch."
