---
title: "Governor-combined baseline documentation"
status: accepted
context: "The docgov scanner requires the expected governance documents to be present."
decision: "Add the missing governance docs in the paths recognized by docgov."
consequences: "The documentation inventory becomes visible to Governor and the repo gains baseline governance coverage."
---

# Context

Governor-combined needs a minimal baseline of governance documentation so the docgov layer can report something useful.

## Decision

We will keep the required document types in the repository and maintain them alongside code changes.

## Consequences

- The doc scanner can recognize the repository.
- Missing documentation becomes a visible maintenance signal.
