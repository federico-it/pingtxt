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
}

// Hello is sent by an agent right after connecting.
type Hello struct {
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// Welcome confirms the agent identity.
type Welcome struct {
	AgentID  int64  `json:"agent_id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}
