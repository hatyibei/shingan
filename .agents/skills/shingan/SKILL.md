```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview
This skill provides guidance on contributing to the `shingan` TypeScript codebase. It covers file organization, code style, commit conventions, and contribution workflows—especially around proposing new features or documentation via pull request templates. The repository is framework-agnostic and emphasizes clarity, maintainability, and structured collaboration.

## Coding Conventions

### File Naming
- Use **kebab-case** for all filenames.
  - **Example:**  
    ```
    my-feature-file.ts
    another-helper.test.ts
    ```

### Import Style
- Use **relative imports** for referencing modules within the project.
  - **Example:**
    ```typescript
    import { myHelper } from './utils/my-helper';
    ```

### Export Style
- Use **named exports** for all modules.
  - **Example:**
    ```typescript
    // In my-helper.ts
    export function myHelper() { /* ... */ }
    ```

### Commit Messages
- Follow **conventional commit** format.
- Common prefix: `docs`
- Keep commit messages concise (average ~49 characters).
  - **Example:**
    ```
    docs: update README with new workflow
    ```

## Workflows

### add-pr-template-draft
**Trigger:** When you want to propose or document a new feature, fix, or documentation change for `shingan-lint` via a PR template.  
**Command:** `/new-pr-template`

1. **Create a new PR template draft:**
   - Add a markdown file in `drafts/shingan-lint/prs/`.
   - Name the file with a sequential number and a descriptive name.
     - **Example:**  
       ```
       drafts/shingan-lint/prs/003-add-lint-rule.md
       ```
2. **Index the new template:**
   - Update `drafts/shingan-lint/README.md` to reference or index the new PR template.
     - **Example addition to README.md:**
       ```
       - [003-add-lint-rule.md](prs/003-add-lint-rule.md): Propose new lint rule
       ```
3. **Open a Pull Request:**
   - Commit your changes with a conventional commit message.
   - Open a PR for review and upstream contribution.

## Testing Patterns

- Test files use the `*.test.*` naming pattern.
  - **Example:**  
    ```
    utils.test.ts
    ```
- The testing framework is not explicitly specified; follow project conventions or consult maintainers for guidance.
- Place test files alongside the modules they test or in a dedicated test directory as per project structure.

## Commands

| Command           | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| /new-pr-template  | Start a new PR template draft for shingan-lint contributions   |
```
