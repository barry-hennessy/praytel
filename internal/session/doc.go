// Package session runs commands while collecting and querying their telemetry.
// It is an in-process lifecycle layer intended for long-running, transport-neutral
// servers. It never invokes the Praytel executable and never exposes a child's
// inherited environment or raw command output.
package session
