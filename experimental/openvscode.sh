export WAYLAND_DISPLAY=$(ls /run/user/$(id -u)/wayland-* | head -n1 | xargs -n1 basename)

glmark2-wayland -s 1280x720 &

#env OZONE_PLATFORM=wayland \
#    code --ozone-platform=wayland --enable-features=UseOzonePlatform &

