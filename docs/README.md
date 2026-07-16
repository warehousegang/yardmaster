# Yardmaster Documentation

This directory contains developer and operator documentation that is too detailed
for the project README.

## Developer Guides

- [Architecture](architecture.md): Components, package boundaries, and data flow.
- [Controllers](controllers.md): Watches, reconciliation behavior, and finding
  lifecycle.
- [DispatchFinding API](dispatchfinding.md): Custom resource schema, generation,
  and compatibility.
- [Development](development.md): Local setup, tests, code generation, and common
  change workflows.

## Operator Guides

- [Operations](operations.md): Deployment, configuration, observability, and
  troubleshooting.
- [Production demo](prod-demo.md): Safe steps for trying Yardmaster against a
  real cluster.

## Images

The `images/` directory contains screenshots referenced by the project README and
other documentation. Keep images only when they support a specific document, and
give them descriptive names.
