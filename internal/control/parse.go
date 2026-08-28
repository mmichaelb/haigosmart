// Package control turns operator input into validated commands and dispatches
// them to bulbs.
package control

import (
	"fmt"
	"strconv"
	"strings"
)

// Action is what a command does.
type Action uint8

// The command set, per contracts/tui-commands.md.
const (
	ActionList Action = iota
	ActionOn
	ActionOff
	ActionBrightness
	ActionColor
	ActionColorTemp
	ActionName
	ActionInfo
	ActionHelp
	ActionQuit
)

// Command is one parsed operator intent.
type Command struct {
	Action Action
	Target string
	Number int    // brightness or colour temperature
	Text   string // new name, or the colour as typed
}

// Verbs is the list of accepted commands, in the order help shows them.
var Verbs = []string{"list", "on", "off", "bri", "color", "temp", "name", "info", "help", "quit"}

// ErrEmpty means the operator submitted nothing; it is not worth an error line.
var ErrEmpty = fmt.Errorf("empty command")

// Parse turns a line of input into a command. A malformed command is an error
// and changes no bulb state.
func Parse(line string) (Command, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Command{}, ErrEmpty
	}
	verb, args := strings.ToLower(fields[0]), fields[1:]

	switch verb {
	case "list", "ls":
		return Command{Action: ActionList}, nil
	case "help", "?":
		c := Command{Action: ActionHelp}
		if len(args) > 0 {
			c.Text = args[0]
		}
		return c, nil
	case "quit", "exit":
		return Command{Action: ActionQuit}, nil
	case "on", "off", "info":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("%s needs exactly one bulb: `%s <bulb>`", verb, verb)
		}
		action := map[string]Action{"on": ActionOn, "off": ActionOff, "info": ActionInfo}[verb]
		return Command{Action: action, Target: args[0]}, nil
	case "bri", "temp":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("%s needs a bulb and a value: `%s <bulb> <%s>`", verb, verb, valueHint(verb))
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return Command{}, fmt.Errorf("%s must be a whole number, got %q", valueHint(verb), args[1])
		}
		action := ActionBrightness
		if verb == "temp" {
			action = ActionColorTemp
		}
		return Command{Action: action, Target: args[0], Number: n}, nil
	case "color", "colour":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("color needs a bulb and a colour: `color <bulb> <#RRGGBB|name>`")
		}
		return Command{Action: ActionColor, Target: args[0], Text: args[1]}, nil
	case "name", "rename":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("name needs a bulb and a new name: `name <bulb> <a-name>`")
		}
		return Command{Action: ActionName, Target: args[0], Text: args[1]}, nil
	default:
		return Command{}, fmt.Errorf("unknown command %q. commands: %s", fields[0], strings.Join(Verbs, " "))
	}
}

func valueHint(verb string) string {
	if verb == "temp" {
		return "temperature"
	}
	return "brightness"
}

// colorNames are the colours accepted by name. Everything else must be hex.
var colorNames = map[string]struct{}{
	"red": {}, "green": {}, "blue": {}, "white": {}, "warmwhite": {},
	"yellow": {}, "cyan": {}, "magenta": {}, "orange": {}, "purple": {},
}

// validColor reports whether the operator's colour is well formed. It does not
// mean any bulb here can display it; that is a capability question.
func validColor(text string) bool {
	if _, ok := colorNames[strings.ToLower(text)]; ok {
		return true
	}
	if len(text) == 7 && text[0] == '#' {
		if _, err := strconv.ParseUint(text[1:], 16, 32); err == nil {
			return true
		}
	}
	return false
}
