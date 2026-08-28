package control

import "strings"

// helpLines is the on-screen reference. It must be enough on its own for a
// first-time operator to list bulbs and change one (SC-007).
var helpLines = []struct{ verb, args, what string }{
	{"list", "", "show every bulb: name, id, status, power, brightness, temperature"},
	{"on", "<bulb>", "turn a bulb on"},
	{"off", "<bulb>", "turn a bulb off"},
	{"bri", "<bulb> <0-100>", "set brightness; 0 switches the bulb off"},
	{"temp", "<bulb> <0-100>", "set white warmth: 0 is warmest, 100 is coolest"},
	{"color", "<bulb> <#RRGGBB|name>", "set colour (only on bulbs that have it)"},
	{"name", "<bulb> <a-name>", "name a bulb; naming a new one adopts it so you can control it"},
	{"info", "<bulb>", "everything known about one bulb"},
	{"help", "[command]", "this list, or detail on one command"},
	{"quit", "", "save and exit"},
}

// Help renders the command reference, or detail on one verb.
func Help(verb string) string {
	var b strings.Builder
	if verb != "" {
		for _, h := range helpLines {
			if h.verb == verb {
				b.WriteString(h.verb + " " + h.args + "\n  " + h.what)
				return b.String()
			}
		}
		return "no such command: " + verb + ". run `help` for the list"
	}
	b.WriteString("<bulb> is a name or id; a unique prefix works too.\n")
	b.WriteString("A new bulb shows as discovered — `name` it once and it becomes controllable.\n\n")
	for _, h := range helpLines {
		line := h.verb
		if h.args != "" {
			line += " " + h.args
		}
		b.WriteString(pad(line, 28) + h.what + "\n")
	}
	b.WriteString("\nkeys: ↑/↓ history · PgUp/PgDn scroll the feed · Ctrl+C quit")
	return b.String()
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}
