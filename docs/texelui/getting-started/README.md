# Getting Started with TexelUI

Welcome to TexelUI! This section will help you get up and running quickly.

## What You'll Learn

1. **[Installation](installation.md)** - How to install and build TexelUI
2. **[Quickstart](quickstart.md)** - Create your first app in 5 minutes
3. **[Hello World](hello-world.md)** - A simple example with full explanation

## Prerequisites

- **Go 1.24+** installed on your system
- Basic familiarity with Go programming
- A terminal that supports 256 colors (most modern terminals do)

## Quick Path

If you're in a hurry, here's the fastest path to a working app:

```bash
# Clone and build
git clone https://github.com/your-org/texelation.git
cd texelation
make build-apps

# Run the demo
./bin/texelui-demo
```

You should see a tabbed interface with input fields, buttons, and other widgets. Use Tab to navigate between elements.

## Which Mode Should You Use?

TexelUI supports two runtime modes:

| Mode | Best For | Documentation |
|------|----------|---------------|
| **Standalone** | Simple tools, testing, learning | [Standalone Mode](../integration/standalone-mode.md) |
| **TexelApp** | Full Texelation integration | [TexelApp Mode](../integration/texelapp-mode.md) |

If you're just getting started, **standalone mode** is simpler and recommended for learning.

## Next Steps

1. Start with the [Installation Guide](installation.md)
2. Follow the [Quickstart](quickstart.md) to build your first app
3. Explore the [Tutorials](../tutorials/README.md) for more advanced examples

---

**Need help?** Check the [main documentation](../README.md) or file an issue on GitHub.
