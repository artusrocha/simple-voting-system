FROM docker.io/library/nginx:1.27-alpine

RUN apk add --no-cache gettext

COPY nginx.conf /etc/nginx/custom/default.conf.template
COPY nginx-benchmark-lb.conf /etc/nginx/custom/benchmark-lb.conf.template
COPY index.html /usr/share/nginx/html/index.html
COPY vote.html /usr/share/nginx/html/vote.html
COPY admin.html /usr/share/nginx/html/admin.html
COPY config.js.template /usr/share/nginx/html/config.js.template
COPY styles.css /usr/share/nginx/html/styles.css
COPY js/ /usr/share/nginx/html/js/
COPY css/ /usr/share/nginx/html/css/
COPY docker-entrypoint.d/40-render-config.sh /docker-entrypoint.d/40-render-config.sh
RUN chmod +x /docker-entrypoint.d/40-render-config.sh

EXPOSE 80
