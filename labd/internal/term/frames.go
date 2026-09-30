package term

import "encoding/json"

// Control frames (JSON text frames). Binary frames carry terminal bytes both ways.
// Spec "Terminal gateway → Protocol"; README.md documents the protocol as implemented.

// clientFrame is any client → server control frame: resize, extend or ping.
type clientFrame struct {
	Type string `json:"type"`
	Cols uint32 `json:"cols,omitempty"`
	Rows uint32 `json:"rows,omitempty"`
}

type queuedFrame struct {
	Type     string `json:"type"` // "queued"
	Position int    `json:"position"`
}

type stateFrame struct {
	Type   string `json:"type"` // "state"
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type ttlFrame struct {
	Type            string `json:"type"` // "ttl"
	IdleRemainingS  int    `json:"idle_remaining_s"`
	HardRemainingS  int    `json:"hard_remaining_s"`
	ExtendAvailable bool   `json:"extend_available"`
}

type warnFrame struct {
	Type    string `json:"type"` // "warn"
	Message string `json:"message"`
}

type extendFrame struct {
	Type string `json:"type"` // "extend"
	OK   bool   `json:"ok"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only fixed structs above are marshalled
	}
	return b
}
