```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview

This skill covers the core development patterns and workflows for the `shingan` Go codebase. It details conventions for file structure, code style, and commit practices, as well as step-by-step guides for implementing new features and fixing bugs. The repository is organized into CLI, domain, and infrastructure layers, with a focus on test coverage and documentation.

## Coding Conventions

- **File Naming:**  
  Use `snake_case` for all Go source files.
  ```
  // Good
  cli/command_parser.go
  domain/user_service.go

  // Bad
  cli/CommandParser.go
  domain/UserService.go
  ```

- **Import Style:**  
  Use relative imports within the module.
  ```go
  import (
      "shingan/domain"
      "shingan/infrastructure/db"
  )
  ```

- **Export Style:**  
  Use named exports for functions, types, and variables.
  ```go
  // domain/user_service.go
  package domain

  type UserService struct { ... }

  func NewUserService() *UserService { ... }
  ```

- **Commit Patterns:**  
  - Use prefixes like `feat:` for features and `fix:` for bugfixes.
  - Keep commit messages concise (~68 characters).
  ```
  feat: add user login command to CLI
  fix: correct DB connection leak in infra layer
  ```

## Workflows

### Feature Implementation with CLI and Infra
**Trigger:** When adding a new feature with end-to-end support (CLI, domain, infra, docs, tests).  
**Command:** `/new-feature`

1. **Add or modify CLI command implementation and its tests**
    - Implement new CLI command in `cli/feature_name.go`
    - Add tests in `cli/feature_name_test.go`
    ```go
    // cli/add_user.go
    func AddUserCmd() { ... }
    ```
2. **Add or modify domain logic and its tests**
    - Implement business logic in `domain/feature_name.go`
    - Add tests in `domain/feature_name_test.go`
    ```go
    // domain/user_service.go
    func (s *UserService) AddUser(...) error { ... }
    ```
3. **Add or modify infrastructure layer for persistence and its tests**
    - Implement persistence in `infrastructure/db/feature_name.go`
    - Add tests in `infrastructure/db/feature_name_test.go`
    ```go
    // infrastructure/db/user_repo.go
    func (r *UserRepo) SaveUser(...) error { ... }
    ```
4. **Update or add documentation**
    - Update `docs/` and architectural decision records (`shingan-adr.md`)
5. **Update `CHANGELOG.md`**
    - Summarize the new feature and its impact

### Bugfix with Test
**Trigger:** When fixing a bug and ensuring it is covered by tests.  
**Command:** `/bugfix`

1. **Update CLI or infrastructure code to fix the bug**
    - Locate and fix the bug in `cli/*.go` or `infrastructure/*/*.go`
    ```go
    // cli/add_user.go
    // Fix: handle empty username case
    if username == "" {
        return errors.New("username required")
    }
    ```
2. **Add or update relevant tests to cover the bug scenario**
    - Write or update tests in `cli/*_test.go` or `infrastructure/*/*_test.go`
    ```go
    // cli/add_user_test.go
    func TestAddUser_EmptyUsername(t *testing.T) { ... }
    ```

## Testing Patterns

- **Test File Naming:**  
  Test files use the pattern `*_test.go` and are placed alongside implementation files.
  ```
  cli/add_user.go
  cli/add_user_test.go
  ```

- **Test Framework:**  
  No explicit framework detected; use Go's standard `testing` package.
  ```go
  import "testing"

  func TestAddUser(t *testing.T) {
      // test logic
  }
  ```

- **Test Coverage:**  
  Tests are expected for CLI, domain, and infrastructure layers, especially for new features and bugfixes.

## Commands

| Command       | Purpose                                                      |
|---------------|--------------------------------------------------------------|
| /new-feature  | Start a new feature with CLI, domain, infra, docs, and tests |
| /bugfix       | Fix a bug and add/update regression tests                    |
```
