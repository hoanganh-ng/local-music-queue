# Build stage
FROM node:20-alpine AS build-stage

ARG VITE_GOOGLE_CLIENT_ID
ENV VITE_GOOGLE_CLIENT_ID=$VITE_GOOGLE_CLIENT_ID
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
# R14c: build-time cutover mode. Supplied by docker-compose from the single
# operator input ROOM_CUTOVER_AUTHORITATIVE so the bundle is always baked
# with the same mode the backend runs with. Defaults to false (pre-cutover).
ARG VITE_ROOM_CUTOVER_AUTHORITATIVE=false
ENV VITE_ROOM_CUTOVER_AUTHORITATIVE=$VITE_ROOM_CUTOVER_AUTHORITATIVE

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm install

# Copy source code
COPY . .

# Build the application
RUN npm run build

# Production stage
FROM nginx:alpine AS production-stage

# Install certbot and cronie for Let's Encrypt certificates
RUN apk add --no-cache \
    python3 \
    py3-pip \
    certbot \
    cronie \
    openssl && \
    pip3 install --break-system-packages certbot-dns-duckdns

# Copy the build output from the build stage
COPY --from=build-stage /app/dist /usr/share/nginx/html

# Copy the nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy certificate setup script and entrypoint script
COPY setup-certs.sh /setup-certs.sh
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /setup-certs.sh /docker-entrypoint.sh

# Expose both HTTP and HTTPS
EXPOSE 80 443

# Use entrypoint script to generate certs and start nginx
CMD ["/docker-entrypoint.sh"]
