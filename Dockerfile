# make build-all
FROM debian:bookworm-slim
COPY dist/localsend-monitor-linux-amd64 /usr/local/bin/localsend-monitor
RUN chmod +x /usr/local/bin/localsend-monitor 
ENTRYPOINT ["/usr/local/bin/localsend-monitor"]
