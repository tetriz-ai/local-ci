# git
Provides helpers to sync `refs/notes/commits` from the remote Git repository.
Handles non-fast-forward rejections by retrying with `--force` and gracefully skips when no remote notes ref exists.
