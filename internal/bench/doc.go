// Package bench measures token, latency, and tool-call efficiency of
// the codesearch MCP tools against a POSIX-bash baseline by driving a
// real Claude agent loop per task and aggregating results.
package bench

import _ "github.com/anthropics/anthropic-sdk-go"
