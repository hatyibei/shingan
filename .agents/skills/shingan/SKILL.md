```markdown
# shingan Development Patterns

> Auto-generated skill from repository analysis

## Overview
This skill teaches the core development patterns and conventions used in the `shingan` Python repository. You'll learn about file naming, import/export styles, commit conventions, and how to write and run tests. The guide also provides step-by-step workflows and suggested commands to streamline your development process.

## Coding Conventions

### File Naming
- Use **snake_case** for all Python files.
  - Example: `data_loader.py`, `utils_helper.py`

### Import Style
- Use **relative imports** within the package.
  - Example:
    ```python
    from .utils import process_data
    ```

### Export Style
- Use **named exports** (explicitly define what is exported).
  - Example:
    ```python
    __all__ = ["process_data", "DataLoader"]
    ```

### Commit Patterns
- Follow **conventional commits** with the `fix` prefix for bug fixes.
  - Example:
    ```
    fix: handle edge case in data parsing
    ```

## Workflows

### Making a Code Change
**Trigger:** When you need to add a feature or fix a bug  
**Command:** `/make-change`

1. Create a new branch for your change.
2. Make your code changes following the coding conventions.
3. Add or update tests as needed.
4. Run tests to ensure everything passes.
5. Commit your changes using the conventional commit style (e.g., `fix: ...`).
6. Push your branch and open a pull request.

### Running Tests
**Trigger:** Before pushing changes or merging a pull request  
**Command:** `/run-tests`

1. Identify test files (they follow the `*.test.*` pattern).
2. Run each test file using your preferred Python test runner.
   - Example:
     ```bash
     python path/to/module.test.py
     ```
3. Verify all tests pass before proceeding.

## Testing Patterns

- Test files are named with the pattern `*.test.*` (e.g., `utils.test.py`).
- The specific test framework is **unknown**; tests may be run directly or with a standard Python test runner.
- Place test files alongside the modules they test or in a dedicated test directory.

**Example test file:**
```python
# utils.test.py
from .utils import process_data

def test_process_data():
    assert process_data([1, 2, 3]) == [2, 3, 4]
```

## Commands
| Command       | Purpose                                   |
|---------------|-------------------------------------------|
| /make-change  | Start the code change workflow            |
| /run-tests    | Run all test files in the repository      |
```
