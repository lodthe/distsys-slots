FROM node:22-alpine
RUN apk add --no-cache chromium
RUN npm install -g playwright@1.55.0
ENV NODE_PATH=/usr/local/lib/node_modules
WORKDIR /work
