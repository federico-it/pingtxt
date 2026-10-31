// Package protocol defines the messages exchanged between hub and agents over
// a WebSocket. The agent always dials out to the hub, so it works behind NAT
// and firewalls without opening ports.
package protocol

import "github.com/federico-it/pingtxt/internal/check"

// Message types.
const (
	TypeHello   = "hello"   // agent -> hub, first message after connect
	TypeWelcome = "welcome" // hub -> agent, accepts the hello
	TypeJob     = "job"     // hub -> agent
	TypeResult  = "result"  // agent -> hub
	// TypeUpdate asks the agent to replace its binary with the one the hub
	// bundles (hub -> agent).
	TypeUpdate = "update"
	// TypeUpdateStatus reports a failed self-update (agent -> hub). A
	// successful update needs no message: the agent reconnects with the new
	// version.
	TypeUpdateStatus = "update_status"
)

// Path is the WebSocket endpoint on the hub.
const Path = "/api/agent/ws"

// Message is the envelope for all frames.
type Message struct {
	Type    string        `json:"type"`
	Hello   *Hello        `json:"hello,omitempty"`
	Welcome *Welcome      `json:"welcome,omitempty"`
	Job     *check.Job    `json:"job,omitempty"`
	Result  *check.Result `json:"result,omitempty"`

	Update       *Update       `json:"update,omitempty"`
	UpdateStatus *UpdateStatus `json:"update_status,omitempty"`
}

// Hello is sent by an agent right after connecting.
type Hello struct {
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	// SelfUpdate is true when the agent can replace its own binary.
	// Agents older than this field never set it.
	SelfUpdate bool `json:"self_update,omitempty"`
	// NoUpdateReason says why SelfUpdate is false, e.g. "container".
	NoUpdateReason string `json:"no_update_reason,omitempty"`
}

// Welcome confirms the agent identity.
type Welcome struct {
	AgentID  int64  `json:"agent_id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// Update tells the agent where to download the new binary. Path is relative
// to the hub URL the agent connects to; SHA256 is the hex digest the
// download must match.
type Update struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

// UpdateStatus reports the outcome of a self-update.
type UpdateStatus struct {
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
	// RetrySoon marks a failure that should clear up by itself within
	// minutes, such as a release whose files are still being uploaded.
	RetrySoon bool `json:"retry_soon,omitempty"`
}

// Reasons an agent cannot update itself.
const (
	NoUpdateContainer   = "container"    // runs in a container: update the image
	NoUpdateDisabled    = "disabled"     // turned off on the agent
	NoUpdateNotWritable = "not_writable" // the binary or its folder is read-only
	NoUpdateUnsupported = "unsupported"  // OS not supported
	NoUpdateInsecure    = "insecure_hub" // hub reached over plain http on a public address
)
