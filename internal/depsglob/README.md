# deps/pkg
Provides a thin abstraction (`IFileGlobFilter`) over `filepath.Glob`.
Used in the `deps` tool's impacted-directory logic, enabling the filesystem glob to be swapped out in tests.
