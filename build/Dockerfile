FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
COPY . /go/src/github.com/openshift/configure-alertmanager-operator
WORKDIR /go/src/github.com/openshift/configure-alertmanager-operator
RUN make gobuild

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
ENV OPERATOR_BIN=configure-alertmanager-operator

WORKDIR /root/
COPY --from=builder /go/src/github.com/openshift/configure-alertmanager-operator/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
LABEL io.openshift.managed.name="configure-alertmanager-operator" \
      io.openshift.managed.description="Operator to configure Alertmanager with PagerDuty and Dead Man's Snitch."
