# App Manifest Examples

## Wrapper App: htop

File: `~/.config/texelation/apps/htop/manifest.json`

```json
{
  "name": "htop",
  "displayName": "System Monitor",
  "description": "Interactive process viewer",
  "version": "1.0.0",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "htop",
  "icon": "📊",
  "category": "system",
  "author": "texelation"
}
```

## Wrapper App: vim

File: `~/.config/texelation/apps/vim/manifest.json`

```json
{
  "name": "vim",
  "displayName": "Vim Editor",
  "description": "Modal text editor",
  "version": "1.0.0",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "vim",
  "args": ["+set mouse=a"],
  "icon": "📝",
  "category": "editor",
  "author": "texelation"
}
```

## Wrapper App: btop

File: `~/.config/texelation/apps/btop/manifest.json`

```json
{
  "name": "btop",
  "displayName": "Modern Monitor",
  "description": "Resource monitor with a nice interface",
  "version": "1.0.0",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "btop",
  "icon": "📈",
  "category": "system",
  "author": "texelation",
  "tags": ["monitor", "system", "performance"]
}
```

## Wrapper App: Python REPL

File: `~/.config/texelation/apps/python/manifest.json`

```json
{
  "name": "python",
  "displayName": "Python REPL",
  "description": "Interactive Python interpreter",
  "version": "1.0.0",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "python3",
  "icon": "🐍",
  "category": "dev",
  "author": "texelation",
  "tags": ["python", "repl", "interpreter"]
}
```

## External App (Future)

File: `~/.config/texelation/apps/mycalc/manifest.json`

```json
{
  "name": "mycalc",
  "displayName": "Calculator",
  "description": "Simple calculator app",
  "version": "1.0.0",
  "type": "external",
  "binary": "./mycalc",
  "icon": "🧮",
  "category": "utility",
  "author": "username",
  "homepage": "https://example.com/mycalc"
}
```

## Installation

To install an app:

```bash
# Create app directory
mkdir -p ~/.config/texelation/apps/htop

# Create manifest
cat > ~/.config/texelation/apps/htop/manifest.json <<'EOF'
{
  "name": "htop",
  "displayName": "System Monitor",
  "description": "Interactive process viewer",
  "version": "1.0.0",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "htop",
  "icon": "📊",
  "category": "system"
}
EOF

# Reload apps in texel-server
killall -HUP texel-server
```

The app will now appear in the launcher!
