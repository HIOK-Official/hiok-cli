# The hiok CLI with what pipeline steps reach for alongside it: docker (for
# registry push), ssh (vm ssh), kubectl (kubernetes), bash, curl, git and jq.
# For any CI that runs jobs in containers — GitLab, Bitbucket, Jenkins, CircleCI,
# Drone, Tekton … — as ghcr.io/hiok-official/hiok.
FROM docker:27-cli
ARG TARGETARCH
ARG KUBECTL_VERSION=v1.31.1
RUN apk add --no-cache bash curl git jq openssh-client ca-certificates \
 && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
 && chmod +x /usr/local/bin/kubectl
COPY hiok /usr/local/bin/hiok
ENTRYPOINT []
CMD ["hiok"]
