FROM golang:1.11.5-stretch as built

ADD . /genesis

RUN cd /genesis &&\
    go get || \
    go build

FROM ubuntu:latest as final

RUN mkdir -p /genesis && apt-get update && apt-get install -y openssh-client ca-certificates
WORKDIR /genesis

COPY --from=built /genesis/resources /genesis/resources
COPY --from=built /genesis/config.json /genesis/config.json
COPY --from=built /genesis/genesis /genesis/genesis

RUN ln -s /genesis/resources/geth/ /genesis/resources/ethereum

ENTRYPOINT ["/genesis/genesis"]