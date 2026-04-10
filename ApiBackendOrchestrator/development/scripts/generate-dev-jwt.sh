#!/usr/bin/env bash
# =============================================================================
# generate-dev-jwt.sh — Generate RSA key pair for local JWT development
# =============================================================================
# Creates an RSA-2048 key pair in the keys/ directory (relative to this script).
# The public key is used by orch-api to validate JWT tokens.
# The private key can be used by a test script or UOM stub to sign tokens.
#
# Usage:
#   bash scripts/generate-dev-jwt.sh
#
# Output:
#   keys/jwt-private.pem   — RSA private key (for signing tokens)
#   keys/jwt-public.pem    — RSA public key (mounted into orch-api container)
#
# This script is idempotent: it will NOT overwrite existing keys.
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYS_DIR="${SCRIPT_DIR}/../keys"

mkdir -p "${KEYS_DIR}"

PRIVATE_KEY="${KEYS_DIR}/jwt-private.pem"
PUBLIC_KEY="${KEYS_DIR}/jwt-public.pem"

if [ -f "${PUBLIC_KEY}" ] && [ -f "${PRIVATE_KEY}" ]; then
    echo "JWT key pair already exists:"
    echo "  Private: ${PRIVATE_KEY}"
    echo "  Public:  ${PUBLIC_KEY}"
    echo "Delete both files and re-run this script to regenerate."
    exit 0
fi

echo "Generating RSA-2048 key pair for local JWT development..."

# Generate private key.
openssl genrsa -out "${PRIVATE_KEY}" 2048 2>/dev/null

# Extract public key.
openssl rsa -in "${PRIVATE_KEY}" -pubout -out "${PUBLIC_KEY}" 2>/dev/null

# Restrict permissions on private key.
chmod 600 "${PRIVATE_KEY}"
chmod 644 "${PUBLIC_KEY}"

echo "JWT key pair generated:"
echo "  Private: ${PRIVATE_KEY}"
echo "  Public:  ${PUBLIC_KEY}"
echo ""
echo "The public key is mounted into the orch-api container at /keys/jwt-public.pem."
echo "Use the private key to sign test JWT tokens (e.g., with jwt.io or a test script)."
