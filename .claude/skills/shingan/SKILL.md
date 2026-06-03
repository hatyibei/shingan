```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview

This skill teaches the core development patterns, coding conventions, and collaborative workflows used in the `shingan` Go codebase. It covers how to implement features, fix bugs with regression tests, evolve schemas or APIs, and keep documentation in sync with code. The skill is ideal for contributors aiming for consistency, reliability, and clarity in Go projects following conventional commit practices.

---

## Coding Conventions

**File Naming**
- Use `snake_case` for all file names.
  - Example: `my_feature.go`, `user_handler_test.go`

**Import Style**
- Use **relative imports** within the module.
  - Example:
    ```go
    import (
        "fmt"
        "../utils"
    )
    ```

**Export Style**
- Use **named exports**: Exported functions, types, and variables start with a capital letter.
  - Example:
    ```go
    // Exported function
    func ProcessData(input string) string {
        // ...
    }
    ```

**Commit Messages**
- Follow **conventional commit** format with these prefixes: `fix`, `feat`, `refactor`, `docs`, `test`.
- Keep messages concise (average ~64 chars).
  - Example: `fix: handle nil pointer in user validation`

---

## Workflows

### Regression-Test-Driven Bugfix
**Trigger:** When a bug is discovered, especially one with subtle or cross-platform effects.  
**Command:** `/bugfix-with-regression-test`

1. Identify and fix the bug in the relevant `.go` file(s).
    ```go
    // Before: possible bug
    if user == nil {
        return
    }
    // After: fixed bug
    if user == nil {
        return errors.New("user cannot be nil")
    }
    ```
2. Add or update a regression test in the corresponding `*_test.go` file to cover the bug scenario.
    ```go
    func TestUserNilError(t *testing.T) {
        err := ProcessUser(nil)
        if err == nil {
            t.Fatal("expected error for nil user")
        }
    }
    ```
3. Update `CHANGELOG.md` to document the fix.
4. Update `go.mod`/`go.sum` if dependencies are added or changed.

---

### Feature Implementation with Tests and Docs
**Trigger:** When a new capability or option is added to the system.  
**Command:** `/new-feature`

1. Implement the new feature in the relevant Go source files.
    ```go
    func EnableDarkMode(userID string) error {
        // implementation
    }
    ```
2. Add or update tests to cover the new feature.
    ```go
    func TestEnableDarkMode(t *testing.T) {
        err := EnableDarkMode("user123")
        if err != nil {
            t.Fatal(err)
        }
    }
    ```
3. Update or create documentation files (`docs/*.md`, `README.md`, etc.) to describe the new feature.
4. Update `CHANGELOG.md` to record the new feature.

---

### Rule or Policy Docs Sync
**Trigger:** When a rule or policy exists in code but is missing or outdated in documentation.  
**Command:** `/sync-rule-docs`

1. Identify undocumented or outdated rules/policies.
2. Add or update entries in `README.md` and `README.ja.md`.
3. Create or update detailed `docs/rules/*.md` files for each rule.
4. Update `CHANGELOG.md` to note the documentation change.

---

### Backward-Compatible Schema or API Evolution
**Trigger:** When schema or API changes are needed but must preserve compatibility with existing data or clients.  
**Command:** `/schema-evolution`

1. Update core Go files to support the new schema or API version.
    ```go
    type UserV2 struct {
        User
        EmailVerified bool
    }
    ```
2. Add migration or fallback logic to handle legacy formats.
    ```go
    func MigrateUser(old User) UserV2 {
        return UserV2{User: old, EmailVerified: false}
    }
    ```
3. Update or add tests to cover both new and legacy behaviors.
4. Update documentation and `CHANGELOG.md`.

---

## Testing Patterns

- Test files follow the pattern: `*_test.go`
- Tests are written in Go's standard testing style, using the `testing` package.
- Each bugfix or feature includes corresponding tests.
- Example:
    ```go
    import "testing"

    func TestFeatureX(t *testing.T) {
        result := FeatureX()
        if result != expected {
            t.Errorf("got %v, want %v", result, expected)
        }
    }
    ```

---

## Commands

| Command                      | Purpose                                                      |
|------------------------------|--------------------------------------------------------------|
| /bugfix-with-regression-test | Fix a bug and add a regression test                          |
| /new-feature                 | Implement a new feature with tests and documentation         |
| /sync-rule-docs              | Sync rule or policy documentation with code                  |
| /schema-evolution            | Evolve schema or API in a backward-compatible manner         |
```
