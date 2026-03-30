#!/bin/sh
set -eu

: "${FEATURE_SLIDE_VOTE_MODE:=off}"
: "${FEATURE_INTERACTION_TELEMETRY:=false}"
: "${FEATURE_POW_VOTE:=false}"
: "${APP_PRODUCT_NAME:=Voting Platform}"
: "${APP_TAGLINE:=Secure and observable voting}"
: "${APP_DESCRIPTION:=White-label voting platform with anti-abuse controls and observability.}"
: "${APP_VOTE_PAGE_TITLE:=Voting Platform - Vote}"
: "${APP_ADMIN_PAGE_TITLE:=Voting Platform - Admin}"
: "${APP_VOTE_RATE_LIMIT:=20r/m}"
: "${APP_RESULTS_RETRY_ATTEMPTS:=3}"
: "${APP_RESULTS_RETRY_INTERVAL_MS:=200}"
: "${APP_POST_CONFIRM_DELAY_MS:=500}"
: "${API_UPSTREAM_SCHEME:=http}"
: "${API_UPSTREAM_HOST:=api}"
: "${API_UPSTREAM_PORT:=8080}"
: "${API_EDGE_AUTH_SECRET:=}"
: "${API_EDGE_AUTH_HEADER:=X-App-Edge-Auth}"
: "${NGINX_DEFAULT_TEMPLATE_PATH:=/etc/nginx/custom/default.conf.template}"

if [ -z "${NGINX_DNS_RESOLVER:-}" ]; then
  NGINX_DNS_RESOLVER="$(awk '/^nameserver / { print $2; exit }' /etc/resolv.conf)"
  export NGINX_DNS_RESOLVER
fi

if [ -z "${ASSET_VERSION:-}" ]; then
  ASSET_VERSION="$(date +%s)"
  export ASSET_VERSION
fi

envsubst '${ASSET_VERSION} ${FEATURE_SLIDE_VOTE_MODE} ${FEATURE_INTERACTION_TELEMETRY} ${FEATURE_POW_VOTE} ${APP_PRODUCT_NAME} ${APP_TAGLINE} ${APP_DESCRIPTION} ${APP_VOTE_PAGE_TITLE} ${APP_ADMIN_PAGE_TITLE} ${APP_VOTE_RATE_LIMIT} ${APP_RESULTS_RETRY_ATTEMPTS} ${APP_RESULTS_RETRY_INTERVAL_MS} ${APP_POST_CONFIRM_DELAY_MS}' \
  < /usr/share/nginx/html/config.js.template \
  > /usr/share/nginx/html/config.js

envsubst '${API_UPSTREAM_SCHEME} ${API_UPSTREAM_HOST} ${API_UPSTREAM_PORT} ${API_EDGE_AUTH_SECRET} ${API_EDGE_AUTH_HEADER} ${APP_VOTE_RATE_LIMIT} ${NGINX_DNS_RESOLVER}' \
  < "${NGINX_DEFAULT_TEMPLATE_PATH}" \
  > /etc/nginx/conf.d/default.conf

for file in /usr/share/nginx/html/index.html /usr/share/nginx/html/vote.html /usr/share/nginx/html/admin.html; do
  tmp_file="${file}.tmp"
  envsubst '${ASSET_VERSION} ${APP_PRODUCT_NAME} ${APP_TAGLINE} ${APP_DESCRIPTION} ${APP_VOTE_PAGE_TITLE} ${APP_ADMIN_PAGE_TITLE}' < "$file" > "$tmp_file"
  mv "$tmp_file" "$file"
done
