# TEXEL_SHELL_INTEGRATION_VERSION=9
# Texelterm Shell Integration for Zsh
# Automatically loaded by texelterm - do not modify
#
# This integration provides:
# - OSC 133 shell integration (prompt/command tracking)
# - Environment capture via DCS sequence
# - OSC 7 CWD reporting for session restore

# Function to send environment via DCS sequence
_texel_send_env() {
    local env_base64=$(env | base64 -w0)
    printf '\033Ptexel-env;%s\033\\' "$env_base64"
}

# OSC 133 Shell Integration with Environment Capture
_texel_precmd() {
    local ret=$?
    printf '\033]133;D;%s\007' "$ret"
    _texel_send_env

    # OSC 7 - Report current working directory for session restore
    printf '\033]7;file://%s%s\007' "$(hostname)" "$PWD"

    printf '\033]133;A\007'
}

_texel_preexec() {
    printf '\033]133;C;%s\007' "$1"
}

# Hook into zsh's precmd/preexec system
autoload -Uz add-zsh-hook
add-zsh-hook precmd _texel_precmd
add-zsh-hook preexec _texel_preexec

# Add OSC 133;B to prompt
if [[ "$PS1" != *"133;B"* ]]; then
    PS1="$PS1%{\033]133;B\007%}"
fi
