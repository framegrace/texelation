# TEXEL_SHELL_INTEGRATION_VERSION=9
# Texelterm Shell Integration for Bash
# Automatically loaded by texelterm - do not modify
#
# This integration provides:
# - OSC 133 shell integration (prompt/command tracking)
# - Environment capture for shell restart persistence
# - Per-terminal history isolation
# - OSC 7 CWD reporting for session restore
#
# Environment files (~/.texel-env-$TEXEL_PANE_ID) persist across shell/server restarts.
# Each pane has its own environment file, stable across process restarts.

# Per-terminal history file (isolated by pane ID)
if [[ -n "$TEXEL_PANE_ID" ]]; then
    export HISTFILE="$HOME/.texel-history-$TEXEL_PANE_ID"
fi

# Enable history append mode (never overwrite)
shopt -s histappend

# Track if this is the first prompt (to avoid capturing environment on startup)
_TEXEL_PROMPT_COUNT=0

# Send debug marker to confirm integration loaded
printf '\033]133;D;999\007' 2>/dev/null

# OSC 133 Shell Integration with file-based environment capture
_texel_prompt_command() {
    local ret=$?
    # Send command end marker (but skip on first prompt)
    if (( _TEXEL_PROMPT_COUNT > 0 )); then
        printf '\033]133;D;%s\007' "$ret"

        # Append new history to file immediately (prevents loss on crash)
        history -a

        # Save environment to file for shell/server restart persistence
        # Use >| to force overwrite (bypasses noclobber)
        # Pane-ID-based file persists across shell restarts and server restarts
        if [[ -n "$TEXEL_PANE_ID" ]]; then
            {
                env
                echo "__TEXEL_CWD=$PWD"
            } >| ~/.texel-env-$TEXEL_PANE_ID
        fi
    fi
    # Increment prompt counter
    (( _TEXEL_PROMPT_COUNT++ ))

    # OSC 7 - Report current working directory for session restore
    printf '\033]7;file://%s%s\007' "$(hostname)" "$PWD"

    # Send prompt start marker
    printf '\033]133;A\007'
}

# Setup PROMPT_COMMAND
if [[ -z "$PROMPT_COMMAND" ]]; then
    PROMPT_COMMAND='_texel_prompt_command'
elif [[ "$PROMPT_COMMAND" != *"_texel_prompt_command"* ]]; then
    PROMPT_COMMAND="_texel_prompt_command;$PROMPT_COMMAND"
fi

# Embed Prompt End (OSC 133;B) into PS1
if [[ "$PS1" != *"\033]133;B"* ]]; then
    PS1="$PS1\[\033]133;B\007\]"
fi

# PS0: Embed command text in OSC 133;C
if [[ "$PS0" != *"\033]133;C"* ]]; then
    PS0='\033]133;C;$(HISTTIMEFORMAT="" history 1 | sed "s/^[ ]*[0-9]*[ ]*//")\'$'\007'
fi
