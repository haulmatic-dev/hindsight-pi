#!/bin/bash
set -e

USAGE="Usage: add-hydra-user <username> [ssh-public-key] [--no-sudo] [--github-token TOKEN]"

if [ $# -lt 1 ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    echo "$USAGE" >&2
    exit 1
fi

USERNAME="$1"
shift

SUDO="yes"
SSH_KEY=""
GITHUB_TOKEN=""

while [ $# -gt 0 ]; do
    case "$1" in
        --no-sudo)
            SUDO="no"
            ;;
        --github-token)
            if [ -z "${2:-}" ]; then
                echo "Error: --github-token requires a value" >&2
                exit 1
            fi
            GITHUB_TOKEN="$2"
            shift
            ;;
        *)
            if [ -z "$SSH_KEY" ]; then
                SSH_KEY="$1"
            else
                SSH_KEY="$SSH_KEY $1"
            fi
            ;;
    esac
    shift
done

if id "$USERNAME" > /dev/null 2>&1; then
    echo "User $USERNAME already exists. Adding to required groups if missing."
else
    /usr/sbin/useradd -m -s /bin/bash "$USERNAME"
    echo "Created user $USERNAME with home directory."
fi

# Add to pi group (required for shared Pi config access)
/usr/sbin/usermod -aG pi "$USERNAME"

# Add to sudo group unless --no-sudo
if [ "$SUDO" = "yes" ]; then
    /usr/sbin/usermod -aG sudo "$USERNAME"
    echo "Added $USERNAME to sudo group."
fi

# Set up SSH authorized_keys if key provided
if [ -n "$SSH_KEY" ]; then
    SSH_DIR="/home/$USERNAME/.ssh"
    AUTH_KEYS="$SSH_DIR/authorized_keys"
    mkdir -p "$SSH_DIR"
    echo "$SSH_KEY" >> "$AUTH_KEYS"
    chown -R "$USERNAME:$USERNAME" "$SSH_DIR"
    chmod 700 "$SSH_DIR"
    chmod 600 "$AUTH_KEYS"
    echo "Added SSH public key to $AUTH_KEYS"
fi

# Create Hydra config directory and copy template
HYDRA_DIR="/home/$USERNAME/.hydra"
HYDRA_CONFIG="$HYDRA_DIR/config.yaml"
TOKEN_FILE="$HYDRA_DIR/.github-token"
mkdir -p "$HYDRA_DIR"

if [ ! -f "$HYDRA_CONFIG" ]; then
    cp /etc/skel/.hydra/config.yaml.example "$HYDRA_CONFIG"
    chown "$USERNAME:$USERNAME" "$HYDRA_CONFIG"
    chmod 644 "$HYDRA_CONFIG"
    echo "Created Hydra config template at $HYDRA_CONFIG"
fi

# Create GitHub token file if token provided
if [ -n "$GITHUB_TOKEN" ]; then
    echo "$GITHUB_TOKEN" > "$TOKEN_FILE"
    chown "$USERNAME:$USERNAME" "$TOKEN_FILE"
    chmod 600 "$TOKEN_FILE"
    echo "Created GitHub token file at $TOKEN_FILE"
fi

echo ""
echo "User $USERNAME is set up for Hydra and Pi."
echo "Next steps:"
echo "  1. Set a password (optional if using SSH keys): passwd $USERNAME"
if [ -z "$SSH_KEY" ]; then
    echo "  2. Add SSH public key to: /home/$USERNAME/.ssh/authorized_keys"
fi
echo "  3. Edit Hydra config at: /home/$USERNAME/.hydra/config.yaml"
if [ -z "$GITHUB_TOKEN" ]; then
    echo "  4. Create GitHub token file: /home/$USERNAME/.hydra/.github-token (chmod 600)"
fi
echo "  5. Log out and back in, or run: newgrp pi"
