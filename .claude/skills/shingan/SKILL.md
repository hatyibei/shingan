```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview
This skill teaches the core development patterns and conventions used in the `shingan` TypeScript codebase. You'll learn how to structure files, write imports and exports, follow commit message guidelines, and write tests in the style used by this repository. This guide is ideal for contributors seeking to maintain consistency and quality in their code contributions.

## Coding Conventions

### File Naming
- Use **snake_case** for all file names.
  - Example:  
    ```
    user_service.ts
    data_processor.test.ts
    ```

### Import Style
- Use **relative imports** for referencing modules.
  - Example:
    ```typescript
    import { processData } from './data_processor';
    ```

### Export Style
- Use **named exports** rather than default exports.
  - Example:
    ```typescript
    // In user_service.ts
    export function getUser(id: string) { /* ... */ }
    export const USER_ROLE = 'admin';
    ```

### Commit Messages
- Follow **conventional commit** format.
- Use the `fix` prefix for bug fixes.
  - Example:
    ```
    fix: correct user ID validation in getUser function
    ```

## Workflows

### Writing a Bug Fix
**Trigger:** When you need to fix a bug in the codebase  
**Command:** `/fix-bug`

1. Identify the bug and create a new branch.
2. Make your changes following the coding conventions above.
3. Write or update tests to cover the bug fix.
4. Use a conventional commit message starting with `fix:`.
5. Push your branch and open a pull request.

### Adding a New Module
**Trigger:** When you want to add a new feature or module  
**Command:** `/add-module`

1. Create a new file using snake_case naming.
2. Write your module using named exports.
3. Use relative imports for any dependencies.
4. Add corresponding test files as `*.test.ts`.
5. Commit with a descriptive message (e.g., `feat: add data processor module`).

## Testing Patterns

- Test files use the pattern `*.test.*` (e.g., `user_service.test.ts`).
- The specific testing framework is not detected, but tests should be colocated with or near the modules they test.
- Example test file:
  ```typescript
  // data_processor.test.ts
  import { processData } from './data_processor';

  test('processData returns correct output', () => {
    // ...test implementation
  });
  ```

## Commands
| Command      | Purpose                                 |
|--------------|-----------------------------------------|
| /fix-bug     | Start the workflow for fixing a bug     |
| /add-module  | Start the workflow for adding a module  |
```
