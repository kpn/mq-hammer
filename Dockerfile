FROM alpine
RUN apk --update upgrade && apk add ca-certificates
RUN mkdir /app && chown daemon:daemon /app
USER daemon
COPY ["build/mqhammer", "/app/"]
WORKDIR /app
CMD ["/app/mqhammer"]
