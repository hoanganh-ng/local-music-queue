#!/bin/sh
set -e

# Run certificate setup script
/setup-certs.sh

# Start crond in background for certificate renewal
crond

# Start nginx
exec nginx -g "daemon off;"