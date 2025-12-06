# Texelation Shell Integration for Bash (V7 - Clean History)
# Source this file in your ~/.bashrc to enable automatic title updates.

# Clean up previous traps
trap - DEBUG 2>/dev/null || true

texel_precmd() {
    local ret="$?"
    # OSC 133;D;ret (Command Finished)
    printf '\033]133;D;%s\007' "$ret"
    # OSC 133;A (Prompt Start)
    printf '\033]133;A\007'
}

if [[ -n "$BASH_VERSION" ]]; then
    # Setup PROMPT_COMMAND
    if [[ "$PROMPT_COMMAND" != *"texel_precmd"* ]]; then
        PROMPT_COMMAND="texel_precmd; ${PROMPT_COMMAND}"
    fi

    # Embed Prompt End (OSC 133;B) into PS1 if not present
    if [[ "$PS1" != *"\033]133;B"* ]]; then
        PS1="$PS1\[\033]133;B\007\]"
    fi

    # PS0 Trick: Use history expansion
    # We unset HISTTIMEFORMAT in the subshell to ensure clean output
    # sed removes leading spaces and the history number
    PS0='\[\033]133;C;$(HISTTIMEFORMAT="" history 1 | sed "s/^[ ]*[0-9]*[ ]*//")\007\]'
fi