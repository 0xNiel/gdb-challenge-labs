# labbase — the shared base for every lab image (spec "Lab images → Base image").
# Alpine pinned by digest + gdb + binutils + file; everything else stripped by strip-tools.sh.
# Built on developer machines with images/labbase/build.sh (no CI, ADR 0008).
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

RUN apk add --no-cache gdb binutils file \
 && addgroup -g 1000 lab \
 && adduser -D -u 1000 -G lab -h /home/lab -s /bin/sh lab

# gdb reads $XDG_CONFIG_HOME/gdb/gdbinit; /home/lab is an empty tmpfs at runtime.
COPY gdbinit /etc/lab-config/gdb/gdbinit

# Last: remove every command not on the allow-list, and apk itself.
COPY strip-tools.sh /tmp/strip-tools.sh
RUN sh /tmp/strip-tools.sh

# The sandbox spec sets the runtime env explicitly (labd/sandbox/sandbox-base.json); these
# make `docker run` behave the same for local poking.
ENV HOME=/home/lab \
    XDG_CONFIG_HOME=/etc/lab-config \
    PATH=/usr/bin:/bin \
    PS1='lab$ ' \
    TERM=xterm-256color
USER 1000:1000
WORKDIR /home/lab
CMD ["/bin/sh"]
