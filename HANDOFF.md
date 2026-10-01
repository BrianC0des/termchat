# TermChat security hardening: handoff for other AIs

Date: 2026-09-30. Repo: /home/devchan/Projects/termchat (Go, module `termchat`).
State: all changes below are UNCOMMITTED in the working tree. Do not revert them.
Verify before and after any change: `go build ./... && go test -race -count=1 ./...` (currently all green).
Pre-existing noise, not yours to fix in the same commit: `go vet` "copies lock" warnings (PeerConnection) and gofmt drift in manager.go, protocol.go, model.go, ghbridge.go.

## DONE (do not redo)
- pkg/gitcollab: patch security filter (git numstat -z validation, symlink/.git/abs-path/.. rejection, 15 MB cap), worktree required, GetDirtyFiles via porcelain -z, CaptureDiff hardened, 16-hex patch IDs, capped patch store.
- pkg/ghbridge: termchat /login token passed to gh as GH_TOKEN (env tokens not overridden), 30 s timeout, no interactive prompts, clear "gh not installed" error. Tests in runner_test.go use a fake gh.
- pkg/system updater: private per-user staging dir (updater_staging.go), atomic stage + SHA-256 meta, size caps, tag validation, no InsecureSkipVerify in delta path.
- pkg/system/delta.go ApplyDelta: bounded target size, uint64 bounds math, bounded zstd stream, output never exceeds declared size. Tests in delta_test.go.
- pkg/ghauth SaveToken: 0600 temp file + fsync + rename.
- pkg/network: pre-handshake 64 KB packet limit, 10 s handshake deadline, max 64 pending conns, accept-loop backoff (limits_test.go).
- (session 2) PeerConnection copies removed: GetPeers returns []PeerInfo snapshots (vet clean for ./pkg/...).
- (session 2) Network post-handshake: LAN ID takeover from a different host refused, caps of 256 peers and 8 per host. NOTE: no idle read deadline was added on purpose: no ping is ever sent (MsgTypePing is defined but unused), so quiet peers are legitimate; TCP keepalive (15 s) handles dead peers. The handshake is still unauthenticated (see item 7).
- (session 2) Updater: no InsecureSkipVerify use remains (param exists, all callers pass false).
- (session 2) gitcollab: .gitmodules (incl. GITMOD~1, case, trailing dots) and gitlink 160000 modes are rejected.
- (session 2) ghauth device flow: 30 s client timeout, 1 MB body limits, verification URI must be https on github.com, slow_down wait capped at 60 s. system.OpenURL now only allows http(s) (was passed straight to xdg-open/open, incl. from chat links).
- pkg/ui: radar scan captures cwd and stale results are dropped after /cd (radar_test.go); /apply usage text shows 16-char ID.
- (session 3) pkg/ui: H3 fixed (robust extractDiffBlock handling inner fences/markdown); H5 fixed (eliminated naked goroutine data race on ghAuthRefresh via typed tea.Cmd/Msg); async /pr, /issues, /issue, /ci via tea.Cmd with loading toasts; immediate radar refresh on /apply and /checkout. Tested in gh_async_test.go.
- (session 3) cmd/termchat-dash: go vet copies-lock warnings resolved (*model pointer receivers).
- (session 3) pkg/system updater: updater injectability hooks (overrideDownloadURLs, overrideVersionTag, currentVersionOverride, execPathOverride); removed leftover insecure bool / InsecureSkipVerify; added comprehensive end-to-end integration tests in updater_test.go (download & replace, mirror fallback from 500 errors, corruption rejection, pre-fetched staged binary execution).
- (session 4) Cloud Relay cold-start mitigation & loading indicator:
  - pkg/network: Added `PreWarmRelay(relayURL)` sending async HTTP probe to `/health` to initiate Render free-tier container boot immediately upon application startup and room switches.
  - pkg/network: Added `relayGen uint64` generation counter to `Manager` to safely invalidate stale reconnect loops upon room switches or `LeaveRoom()`.
  - pkg/network: Implemented exponential backoff with live status reporting via `OnRelayStatus(status, connected)` callback during Render cold start delays (~30-60s). Tested in `pkg/network/relay_test.go`.
  - pkg/ui: Added `relayStatus`, `relayConnecting`, and `relaySpinnerIdx` to `Model`; rendered animated braille spinner loading banner (`relayBanner`) with viewport height adjustments in `view.go` and `model.go`.
  - main.go: Triggers `network.PreWarmRelay(*relayFlag)` early on startup.

## OPEN, needs the owner's decision (do not guess)
1. Update signing: updates are unauthenticated (hashes only detect corruption; the mirror supplies them). Proposed: embedded ed25519 public key + detached signature per release. Owner must hold the private key and add a signing step to releases.
2. Token precedence in ghauth.GetToken: env > gh CLI > termchat /login store. A /login token can be silently ignored. Decide if intended.

## OPEN, ready to do (suggested order)
3. pkg/network/protocol.go signing and handshake authentication: unreviewed. The handshake has no auth, so ID spoofing from the SAME host is still possible; the takeover guard only blocks other hosts.
4. Windows path checks (NTFS/8.3 names, drive letters): only tested on Linux.

## Housekeeping
- Commit in separate commits: gitcollab, ghbridge, updater+delta, ghauth, network, ui radar. Do the gofmt -w drift in its own commit.
- Stale vault tasks still CLAIMED (leave unless the owner says close): v2.1.2 release (AppVersion is already v2.1.2), Obsidian callouts, AGY best-practices, First Connection Test.

## Rules for whoever continues
- Add a test for every fix; confirm it fails on the old code where practical.
- Do not weaken caps/limits to make tests pass; tests can shrink package vars (maxUpdateExtracted, handshakeTimeout).
- Report findings honestly, including what was not reviewed.
