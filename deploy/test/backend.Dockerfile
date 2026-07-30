FROM alpine:3.22.1

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
ENV TZ=Asia/Shanghai

COPY uvp-gb28181 /app/uvp-gb28181
COPY resource/database /app/resource/database
COPY resource/public /app/resource/public
RUN mkdir -p /app/resource/logs /app/resource/public/uploads \
    && chmod +x /app/uvp-gb28181

EXPOSE 18978 56002/tcp 56002/udp

ENTRYPOINT ["/app/uvp-gb28181"]
