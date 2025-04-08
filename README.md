# go-upgrade-check

**Preview the potential impact of upgrading a Go dependency on your project *before* you run `go get`.**

## The Problem

Running `go get dependency@latest` or `go get ./...` to upgrade dependencies can sometimes lead to unexpected compilation errors or even subtle runtime changes. This happens when the dependency introduces breaking changes (e.g., removes a function you use, changes a function's signature, modifies an interface).

Finding these issues *after* modifying your `go.mod` and `go.sum` can be disruptive. You might need to revert changes, fix your code under pressure, or pin the dependency to an older version.

## The Solution

`go-upgrade-check` aims to give you a heads-up *before* you modify your project's dependencies. It analyzes how your project uses a specific dependency and compares the definitions (like function signatures and interface methods) between the dependency's old and new versions.

NOTE: It ONLY specifically reports changes or removals for the parts of the dependency's API that **your project actually uses**.

## How it Works

`go-upgrade-check` uses the Source Code Index Format ([SCIP](https://about.sourcegraph.com/scip)) and the `scip-go` indexer.

## Features

*   Detects signature changes in functions used by your project.
*   Detects removed functions/exported symbols used by your project.
*   Focuses **only** on the parts of the dependency API your project consumes, reducing noise.
*   Requires `scip-go` to be installed and available in your `PATH`.

## Installation

Currently, you need to build from source:

```bash
# 1. Ensure scip-go is installed and in your PATH
#    (See: https://github.com/sourcegraph/scip-go#installation)

# 2. Clone this repository
git clone https://github.com/Oloruntobi1/go-upgrade-check.git
cd go-upgrade-check

# 3. Build the binary
go build -o go-upgrade-check .

# 4. (Optional) Move the binary to a location in your PATH
# mv go-upgrade-check /usr/local/bin/
```

## Usage

Run the tool with flags specifying your project, the dependency module path, and the versions to compare.

```bash
go-upgrade-check \
    --project-path="/path/to/your/go/project" \
    --module="github.com/example/dependency" \
    --old-version="v1.2.0" \
    --new-version="v1.5.3"
```

**Flags:**

*   `--project-path`: (Required) Absolute path to the root of your Go project containing the `go.mod` file.
*   `--module`: (Required) The module path of the dependency you want to check (e.g., `github.com/gin-gonic/gin`).
*   `--old-version`: (Required) The version tag/commit/branch of the dependency you are currently using or comparing against (e.g., `v1.8.0`).
*   `--new-version`: (Required) The version tag/commit/branch of the dependency you are considering upgrading to (e.g., `v1.9.1`).

## Example Output

Below some output logs from the tool you should then see the following:

```
The following functions have been changed or removed:
- func ChangingFunction(s string) int -> func ChangingFunction(s string, prefix bool) int
- func DeprecatedFunction(n int) int -> removed

```

## Limitations

*   **Experimental:** This tool is new and may have bugs or inaccuracies.
*   **Requires `scip-go`:** Relies entirely on `scip-go` being installed and working correctly.
*   **Semantic Changes:** Cannot detect changes in logic/behavior if the function/method signature remains identical.
*   **Unexported Symbols:** Does not track changes in unexported symbols, even if they affect the behavior of exported ones you use.
*   **Performance:** Indexing large projects or dependencies can take time. Cloning dependencies also takes time and disk space.

## Future Improvements

*   **Interface Change Detection**: Identify changes to interface definitions that could break implementations.
*   **Method Set Analysis**: Find changes to methods of types you're embedding or extending.
*   **Type Compatibility Analysis**: Detect when type definitions change in incompatible ways.
*   **Structural Type Compatibility**: Recognize when struct fields are added, removed, or modified.
*   **Visual Diff Reports**: Generate visual reports showing API differences.
*   **`go.mod` Integration:** Option to automatically detect the `--old-version` from the project's `go.mod`.
*   **CI/Pre-commit Integration:** Provide guidance or scripts for running checks automatically.
*   **Suggest Replacements:** If a symbol is removed/changed, attempt to find similarly named symbols in the new version as potential replacements.
*   **Performance Optimizations:** Explore caching SCIP indexes for dependencies, potentially parallelizing steps.


## Contributing

We welcome contributions! Please feel free to open an issue or submit a pull request.

## How `go-upgrade-check` fits with `go get`

This tool is designed to be a **preparatory step** *before* you run `go get`. It does **not** replace `go get` and it does **not** modify your `go.mod` or `go.sum` files.

Here's the recommended workflow:

1.  **Identify Potential Upgrade:** You find out a new version of a dependency is available (e.g., via `go list -m -u all`, Dependabot, manual check). Let's say you use `github.com/example/dep` version `v1.2.0` and `v1.3.0` is available.
2.  **Run Pre-Check:** Before touching your `go.mod`, run `go-upgrade-check`:
    ```bash
    go-upgrade-check \
      --project-path="/path/to/your/project" \
      --module="github.com/example/dep" \
      --old-version="v1.2.0" \
      --new-version="v1.3.0"
    ```
3.  **Review Results:** Look at the output.
    *   **No changes reported?** Great! The upgrade is *less likely* to break your build due to API changes you rely on.
    *   **Changes reported?** Examine the specific functions/interfaces listed. Understand how the changes might affect your code. You might need to plan for refactoring *before* or *immediately after* upgrading.
4.  **Decide and Upgrade:** Based on the preview:
    *   If the changes are acceptable or non-existent, proceed with the actual upgrade:
        ```bash
        go get github.com/example/dep@v1.3.0
        ```
    *   If the changes are too significant for now, you can postpone the upgrade or plan the necessary code modifications.
5.  **Verify:** After running `go get`, always run your standard checks:
    ```bash
    go build ./...
    go test ./...
    # Any other integration/lint checks you have
    ```

Think of `go-upgrade-check` like a `terraform plan` for your Go dependency upgrades – it shows you what *might* happen before you apply the change (`go get`).