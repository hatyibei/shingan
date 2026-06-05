```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview

This skill teaches you the core development patterns, coding conventions, and workflows used in the `shingan` TypeScript codebase. The repository focuses on static analysis and security rule extraction for JavaScript/TypeScript code, with a particular emphasis on supporting and testing security rules in the `langgraph-js` parser shim. You'll learn how to add new security rules, create and update test fixtures, and maintain consistent code style across the project.

## Coding Conventions

### File Naming

- Use **snake_case** for file names.
  - Example: `export_langgraphjs_server.mjs`, `prompt_injection_vuln.ts`

### Import Style

- Use **relative imports**.
  - Example:
    ```typescript
    import { analyzeGraph } from './graph_utils';
    ```

### Export Style

- Use **named exports**.
  - Example:
    ```typescript
    export function extractRuleConfig(config: object): RuleConfig { ... }
    ```

### Commit Messages

- Follow **conventional commit** style.
- Prefixes: `feat`, `test`, `docs`
- Example:
  ```
  feat: add prompt injection detection to langgraph-js parser
  ```

## Workflows

### Add Security Rule Support for langgraph-js

**Trigger:** When you want to add support for a new security rule or static analysis in langgraph-js graphs.  
**Command:** `/add-security-rule-support langgraph-js`

1. **Update the parser shim:**
   - Edit `infrastructure/parser/shims/export_langgraphjs_server.mjs` to extract the new rule's config or schema.
   - Example:
     ```javascript
     // Add extraction logic for the new rule
     if (config.type === 'prompt_injection') {
       rules.push(extractPromptInjectionRule(config));
     }
     ```
2. **Add or update test fixtures:**
   - Place new test files in `testdata/langgraphjs/`, using the naming pattern `*_safe.ts` for safe cases and `*_vuln.ts` for vulnerable cases.
   - Example:
     ```
     testdata/langgraphjs/prompt_injection_vuln.ts
     testdata/langgraphjs/prompt_injection_safe.ts
     ```
3. **Update Go test assertions:**
   - Edit or create tests in `infrastructure/parser/langgraphjs_test.go` to verify that the rule fires or does not fire as expected.
   - Example:
     ```go
     func TestPromptInjectionRule(t *testing.T) {
         assertRuleFires(t, "prompt_injection_vuln.ts")
         assertRuleDoesNotFire(t, "prompt_injection_safe.ts")
     }
     ```
4. **Run regression tests:**
   - Ensure no unintended changes to existing fixtures by running the full test suite.

---

### Add Test Fixture and Go Test for langgraph-js Rule

**Trigger:** When you want to add or improve test coverage for a security rule in langgraph-js.  
**Command:** `/add-langgraph-js-test-fixture`

1. **Create or update a test fixture:**
   - Add a new `.ts` file in `testdata/langgraphjs/` (e.g., `prompt_injection_vuln.ts`).
   - Example:
     ```typescript
     // prompt_injection_vuln.ts
     const userInput = getUserInput();
     eval(userInput); // Vulnerable usage
     ```
2. **Update Go test assertions:**
   - Edit `infrastructure/parser/langgraphjs_test.go` to add or broaden test assertions for the new fixture.
   - Example:
     ```go
     func TestPromptInjectionVuln(t *testing.T) {
         assertRuleFires(t, "prompt_injection_vuln.ts")
     }
     ```
3. **Run Go tests:**
   - Execute the tests to verify correct rule behavior.

---

## Testing Patterns

- Test files are named with the pattern `*.test.*`.
- Test fixtures for langgraph-js are stored in `testdata/langgraphjs/` and follow the `*_safe.ts` and `*_vuln.ts` naming convention.
- Go tests in `infrastructure/parser/langgraphjs_test.go` assert rule firing/non-firing for each fixture.
- Example test fixture:
  ```typescript
  // testdata/langgraphjs/prompt_injection_safe.ts
  const safeInput = sanitize(getUserInput());
  eval(safeInput); // Safe usage
  ```

## Commands

| Command                                 | Purpose                                                         |
|------------------------------------------|-----------------------------------------------------------------|
| /add-security-rule-support langgraph-js  | Add support for a new security rule in langgraph-js parser shim |
| /add-langgraph-js-test-fixture           | Add or improve test coverage for a langgraph-js security rule   |
```