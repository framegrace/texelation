# TEXEL_SHELL_INTEGRATION_VERSION=9
# Texelterm Shell Integration for Fish
# Automatically loaded by texelterm - do not modify
#
# This integration provides:
# - OSC 133 shell integration (prompt/command tracking)
# - Environment capture via DCS sequence
# - OSC 7 CWD reporting for session restore

function _texel_send_env
    set -l env_base64 (env | base64 -w0)
    printf '\033Ptexel-env;%s\033\\' "$env_base64"
end

function _texel_prompt_end --on-event fish_prompt
    printf '\033]133;A\007'
end

function _texel_preexec --on-event fish_preexec
    printf '\033]133;C;%s\007' "$argv"
end

function _texel_postexec --on-event fish_postexec
    printf '\033]133;D;%s\007' "$status"
    _texel_send_env

    # OSC 7 - Report current working directory for session restore
    printf '\033]7;file://%s%s\007' (hostname) "$PWD"
end

# Add OSC 133;B to prompt
if not string match -q '*133;B*' -- "$fish_prompt"
    function fish_prompt
        printf '\033]133;B\007'
        # Call original prompt
        if functions -q __texel_original_fish_prompt
            __texel_original_fish_prompt
        end
    end
end
