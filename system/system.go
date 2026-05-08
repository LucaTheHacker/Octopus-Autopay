// Package system exposes OS-native primitives used by the recurring binary:
// screen-readiness detection, desktop notifications, and login-trigger
// schedule installation. Each function has a per-OS implementation in
// system_<goos>.go selected at compile time via build tags.
package system
