package hc

import "strings"

// Machine is another machine the core added (the schema's MachineView),
// with the fields the client reads: its terminals are Name/t7.
type Machine struct {
	Name string `json:"name"`
	// Host is what its core calls itself, its hostname; Version that
	// core's hc version, as last seen.
	Host    string `json:"host"`
	Version string `json:"version"`
	// State is its line now: connecting, connected, needs_login or
	// unreachable; Why says why it is not connected.
	State     string `json:"state"`
	Why       string `json:"why,omitempty"`
	Temporary bool   `json:"temporary,omitempty"`
}

// Machines lists the machines the core added. The core tells no events of
// them: a client asks again to see them change. The methods on machines
// are not among those Uses names: a core whose Hello lacks them has no
// machines.
func (c *Conn) Machines() ([]Machine, error) {
	var r struct {
		Machines []Machine `json:"machines"`
	}
	err := c.Call("machine.list", nil, &r)
	return r.Machines, err
}

// ConnectMachine dials a machine now rather than at its next retry, as
// after a login, returning it as it is then.
func (c *Conn) ConnectMachine(name string) (Machine, error) {
	var m Machine
	err := c.Call("machine.connect", map[string]string{"machine": name}, &m)
	return m, err
}

// MachineOf returns the machine a terminal is on, by its address: "" for
// the core's own.
func MachineOf(address string) string {
	machine, _, _ := strings.Cut(address, "/")
	if machine == address {
		return ""
	}
	return machine
}

// ReadFile reads the first lines, at most limit, of a text file on a
// machine, "" for the core's own: path is absolute, or from cwd.
func (c *Conn) ReadFile(machine, cwd, path string, limit int) (string, error) {
	var r struct {
		Text string `json:"text"`
	}
	params := map[string]any{"path": path, "limit": limit}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if machine != "" {
		params["on"] = machine
	}
	err := c.Call("file.read", params, &r)
	return r.Text, err
}
