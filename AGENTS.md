# Development

- This project uses the Conda environment `stashscrapers`.
- Run every development, test and build command with `conda run -n stashscrapers <command>`; never invoke a global Go, Python, Node or npm directly. In this workstation Conda is `D:\soft\anaconda3\Scripts\conda.exe`.
- Non-interactive shells must not use `conda activate`.
- Keep upstream engine provenance, licenses, deterministic extraction and compatibility tests in sync.
- Treat downloaded community scripts as trusted executable code; never execute unconfigured runtimes or mutate a shared dependency environment.
- User settings and credentials belong in the data directory, outside immutable upstream snapshots.
- Keep the administration UI in Chinese. Do not claim live website compatibility from offline fixture tests.
