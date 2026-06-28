---
type: Decision
title: Automatic codebase indexing and watching daemon
description: Introduce a lightweight daemon and systemd user service to automate codebase discovery, registration, indexing, and live watching.
tags: [operations, automation, daemon, systemd]
timestamp: 2026-06-28T20:35:00+02:00
---

## Context

Using the `agentdb` CLI manually requires users to run `agentdb codebase register`, `agentdb index`, and `agentdb analyze` every time they create a new project. Additionally, keeping the indices updated requires manually launching and managing background `agentdb watch` processes for each project. 

Managing these watches manually is tedious, prone to resource leaks if not cleaned up, and leads to outdated codebase indices if forgotten. We need a way to fully automate project discovery, initial indexing, and filesystem watching with minimal system overhead.

## Decision

We introduce a background daemon (`agentdb-watcher.py`) managed as a systemd user service (`agentdb-watcher.service`). The daemon performs the following:

1. **Discovery**: Periodically scans `~/Projects/` up to a depth of 3 to find Git repositories. It excludes heavy directories like `node_modules` or `.venv` during traversal to keep directory walking under 1ms.
2. **Registration**: Automatically registers newly found repositories with `agentdb`.
3. **Initial Indexing**: Spawns asynchronous indexing and analysis tasks for newly registered repositories.
4. **Watch Management**: Spawns and manages `agentdb watch --analyze` for all registered codebases. It implements a 1.0-second delay between launches to prevent SQLite `SQLITE_BUSY` (database is locked) errors.
5. **Session Lifecycle**: Integrates as a systemd user service that automatically stops all child watch processes when shut down or restarted.

## Alternatives Considered

- **Watch the entire filesystem root for new directories** — too resource-intensive; traversing the entire user home directory recursively would cause constant CPU and I/O load.
- **Trigger indexing via Git hooks or wrapper script (e.g. wrapper for git init)** — fragile; does not handle codebases cloned from remote hosts or projects imported/created by editors/IDE tools directly.
- **Run watch processes inside a single multi-threaded go binary** — requires rewriting `agentdb watch` to support managing multiple concurrent codebase watches internally; a simple external python manager is faster to deploy, less invasive, and utilizes existing CLI interfaces cleanly.

## Consequences

- **Automation**: Newly created git repositories under `~/Projects/` are automatically indexed and kept updated in `agentdb` with zero user intervention.
- **Resource Usage**: Running many parallel Go-based watchers consumes around 15-30 MB of RAM per project. Across 30 codebases, this adds up to ~900 MB max if all are active, but it sits at ~50 MB when idle.
- **SQLite Concurrency**: Spawning multiple processes simultaneously can cause SQLite write locks. We mitigate this by spacing out process start times.
- **inotify Limit**: The kernel's `fs.inotify.max_user_watches` might need to be increased if the number of monitored directories exceeds the system limit.
