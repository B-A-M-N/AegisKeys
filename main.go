// Command aegiskeys is a secure local vault for API provider metadata and
// secrets, with child-process-scoped secret injection for coding agents.
package main

import "aegiskeys/cmd"

func main() {
	cmd.Execute()
}
