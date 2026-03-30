FROM node:22-alpine

RUN npm install -g @typespec/compiler @typespec/http @typespec/rest @typespec/openapi3 @scalar/cli

WORKDIR /typespec

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

CMD ["/entrypoint.sh"]
