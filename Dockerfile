FROM alpine

COPY bin/meshnetd /

RUN chmod +x /meshnetd

ENTRYPOINT ["/meshnetd"]