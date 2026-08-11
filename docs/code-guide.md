---
title: "Governor-combined code documentation guide"
standards:
  - GoDoc for exported Go symbols
  - JSDoc for JavaScript and TypeScript exports
  - RustDoc for public Rust APIs
  - short examples for complex behavior
---

# Code Documentation Guide

## GoDoc

- Document every exported type, function, and method.
- Write comments that explain intent, not restate the signature.
- Keep examples close to the code they explain.

## JSDoc

- Annotate exported functions with parameter and return descriptions.
- Call out side effects, I/O, and asynchronous behavior.

## RustDoc

- Document public modules and types.
- Explain ownership, lifetimes, and error behavior when they matter.

## Naming

- Use descriptive names that match domain language.
- Avoid abbreviations unless the repository already standardizes them.

## Formatting

- Keep comments short and specific.
- Use fenced code blocks for non-trivial examples.

## Examples

- Prefer one compact example over a paragraph of hand-waving.
- Show the failure mode when that failure is easy to misunderstand.
