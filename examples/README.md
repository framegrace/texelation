# Texelation Examples

This directory contains examples and demos that have additional dependencies not required by the main texelation project.

## tviewform

Demonstrates how to embed [tview](https://github.com/rivo/tview) forms into a texel app using a SimulationScreen backend.

### Building

```bash
cd examples
go mod download
go build -o bin/tviewform ./tviewform/cmd/
```

### Running

```bash
./bin/tviewform
```

## Note

These examples use a local replace directive to depend on the parent texelation module, so they will always use your local version.
