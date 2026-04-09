# Implementation Tasks

## Phase 1: New Helpers
- [x] Add `containsAny`, `readFileOnRemote`, `extractJSONField` to `integration_helpers_test.go`
- [x] Add `assertRouterRunning`, `assertRouterNotExists`, `cleanupRouter` to `integration_helpers_test.go`
- [x] Add `waitForGateway`, `findFreePort`, `httpGetStatus` to `integration_helpers_test.go`

## Phase 2: TestRemoteErrorPaths
- [x] Implement `TestRemoteErrorPaths` in `integration_remote_test.go`
- [x] Run and verify error paths test passes (6 subtests)

## Phase 3: TestRemoteMultiAgent
- [x] Implement `TestRemoteMultiAgent` in `integration_remote_test.go`
- [x] Run and verify multi-agent test passes (12 subtests)

## Phase 4: TestRemoteChannelManagement
- [x] Implement `TestRemoteChannelManagement` in `integration_remote_test.go`
- [x] Run and verify channel management test passes (14 subtests)

## Phase 5: TestRemoteConnect
- [x] Implement `TestRemoteConnect` in `integration_remote_test.go`
- [x] Run and verify connect test passes (4 subtests — limited scope due to test infra)

## Phase 6: Full Suite Verification
- [x] Run all 7 remote integration tests together (86s, all pass)
- [x] Run `go vet` and `gofmt` checks (clean)
