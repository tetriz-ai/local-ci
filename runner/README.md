# runner
Orchestrates the full local-ci pipeline: runs all preppers sequentially, then executes checkers.
Collects results, stamps the commit via NotesStamper, and runs post-processors.
