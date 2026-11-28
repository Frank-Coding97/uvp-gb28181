FROM nginx@sha256:b3c656d55d7ad751196f21b7fd2e8d4da9cb430e32f646adcf92441b72f82b14

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --chown=nginx:nginx dist/ /usr/share/nginx/html/

EXPOSE 80
