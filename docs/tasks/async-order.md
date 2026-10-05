# Task: make downloader/installer async ordering identical to Composer's

The installer port reported two ordering divergences (see HANDOFF.md, internal/installer section):
1. `ArchiveDownloader.Install` returns an already-settled promise, so a package's bin warnings / "Install of x failed" line print right after its own "Installing" line; on Unix Composer prints them after all "Installing" lines of the batch (its install promises settle on the next loop tick).
2. The downloader's `then` helper always runs callbacks on a new goroutine, so in `DownloadManager.Update` with a downloader type change the "Removing"/"Installing" lines come from a goroutine and their order is nondeterministic.

Model Composer's event-loop semantics precisely instead of goroutine callbacks: React promises in Composer resolve synchronously when already settled, and Loop::wait drives curl/process completions on the main thread. Design one small, explicit scheduling model (e.g. a run-queue drained by Loop.Wait on the calling goroutine; parallel work — HTTP, extraction into the store — stays on worker goroutines, but every callback that prints, dispatches events or mutates shared state runs on the loop's goroutine, in the order Composer's loop would run it, deterministically). Apply it across internal/util (Promise), internal/util/http (Loop, HttpDownloader), internal/downloader, internal/downloader/vcs and internal/installer so they share it (DRY). Composer's ordering where it is genuinely nondeterministic (completion order of parallel downloads) may be made deterministic in operation order; document where.

Tests: reproduce both divergences as failing tests first (installer + downloader), then fix; all existing tests (incl. internal/composer's 209 installer fixtures) must keep passing; run with -race several times.

Scope: internal/util (promise/loop), internal/util/http (Loop), internal/downloader(+vcs), internal/installer. Other agents are writing internal/command, internal/plugin and cmd/maestro concurrently: keep public APIs stable (or forwarding aliases) and note changes in HANDOFF.md.
Report: the scheduling model, what changed, test/lint status.
