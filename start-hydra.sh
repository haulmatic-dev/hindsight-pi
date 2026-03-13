#!/bin/zsh
# Hydra wrapper - ensures shell environment is loaded

# Source zshrc to get all environment variables
if [ -f "$HOME/.zshrc" ]; then
    source "$HOME/.zshrc"
fi

# Change to hydra directory
cd /Users/buddhi/source-code/personal/hydra

# Export API key explicitly (fallback)
export KIMI_API_KEY="${KIMI_API_KEY:-sk-kimi-PAJXN8wa1cPyJwEDEYI0LoynrIs95I9Zzbsf2zMMcobp3Uc0z4UPqRCY9aJbjzHa}"

# Start hydra with all environment variables
exec ./bin/hydra -config ./hydra-local.yaml "$@"
