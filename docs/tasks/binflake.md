# Task: the plugin-captainhook `vendor/bin` flake

plugin-captainhook (cmd/maestro e2e plugin fixtures) intermittently ends with an empty/missing vendor/bin after `install --no-dev` (warm run); it also fails at older commits. Agents suspected Composer recreates vendor/bin depending on the order its uninstall callbacks finish. Find the truth:
1. Reproduce reliably (loop the fixture; -count, stress), for maestro AND for real Composer 2.10.3 — does Composer's own result vary run to run? Read BinaryInstaller/LibraryInstaller/InstallationManager uninstall + cleanup code paths in .ref/composer and compare with internal/installer.
2. If Composer is deterministic in practice, make maestro produce exactly that. If Composer genuinely varies, make maestro deterministic with the outcome Composer produces in the common case (document why), and make the e2e comparison robust for this case only (precise normalisation, not a blanket ignore).
3. Add a unit/integration test in internal/installer reproducing the root cause.
Scope: internal/installer, internal/util (scheduler) if it's an ordering issue, the e2e fixture. A performance agent and a plugin phase-6 agent work concurrently — coordinate via HANDOFF.md. Commit nothing.
Report: root cause, fix, evidence (loop counts), test/lint status.
