// Package sandbox compiles sandbox-base.json into labd, so a deployed labd cannot be pointed
// at a loosened spec file. The JSON stays at this path because P0, specrun and the docs
// refer to it; this file is the only Go code outside cmd/ and internal/ (labd/README.md).
package sandbox

import _ "embed"

// Base is labd/sandbox/sandbox-base.json. orch.BuildSpec checks the invariants on every use.
//
//go:embed sandbox-base.json
var Base []byte
