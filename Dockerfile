FROM alpine
RUN mkdir /app && chown daemon:daemon /app
USER daemon
COPY ["build/mqhammer", "/app/"]
WORKDIR /app
ENTRYPOINT ["/app/mqhammer"]
