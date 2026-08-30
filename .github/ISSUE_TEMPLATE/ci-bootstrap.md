---
name: CI bootstrap
about: Track the first GitHub Actions workflow for the repository
title: "ci: bootstrap initial GitHub Actions workflow"
labels: ["ci", "chore"]
---

## What

Bootstrap the first GitHub Actions workflow for the repository.

## Scope

- Proto lint
- Go format check
- Go vet
- Go unit test
- Go build

## Workflow Rules

- Follow issue -> branch -> commit -> PR -> GitHub Actions -> merge
- Keep the first version focused on the five checks above
- Trigger on pull requests and pushes to `main`

## Acceptance Criteria

- [ ] Workflow file exists under `.github/workflows/`
- [ ] The five checks run in CI
- [ ] The workflow is suitable for future PR-based development

