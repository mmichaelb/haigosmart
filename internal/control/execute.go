package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/lights"
	"haigosmart/internal/registry"
)

// CommandTimeout is how long to wait for a bulb to confirm a command before
// telling the operator it is still outstanding.
//
// It is a reporting threshold, not a deadline: the command has already been
// delivered, and passing this point changes only what the operator is told. The
// hardware's confirmation latency is wildly uneven — the same 100-to-1
// brightness change was confirmed after four seconds on one attempt and
// nineteen on another — so no fixed number can separate "slow" from "failed".
// Waiting longer would only mean staring at a prompt for longer.
const CommandTimeout = 5 * time.Second

// Result is what the operator sees after a command. Kind maps to the three
// output shapes in contracts/tui-commands.md.
type Result struct {
	Kind ResultKind
	Text string
	// Quit is set when the operator asked to exit.
	Quit bool
}

// ResultKind is one of the three output shapes.
type ResultKind uint8

// The output shapes.
const (
	ResultOK ResultKind = iota
	ResultError
	ResultInfo
)

func (k ResultKind) String() string {
	switch k {
	case ResultError:
		return "error"
	case ResultInfo:
		return "info"
	default:
		return "ok"
	}
}

// Line renders the result in the documented shape.
func (r Result) Line() string { return fmt.Sprintf("%-7s %s", r.Kind.String(), r.Text) }

func okf(format string, args ...any) Result {
	return Result{Kind: ResultOK, Text: fmt.Sprintf(format, args...)}
}

func errf(format string, args ...any) Result {
	return Result{Kind: ResultError, Text: fmt.Sprintf(format, args...)}
}

func infof(format string, args ...any) Result {
	return Result{Kind: ResultInfo, Text: fmt.Sprintf(format, args...)}
}

// Controller is the terminal's adapter over lights.Service. It owns parsing,
// target resolution by name or prefix, and rendering — and nothing else. The
// logic of changing a lamp lives in the service, where Home Assistant reaches it
// too.
type Controller struct {
	svc *lights.Service
	reg *registry.Registry
}

// New returns a controller over the given service. The registry is still needed
// here for the things that are genuinely terminal concerns: resolving a typed
// prefix to a lamp, and renaming.
func New(svc *lights.Service, reg *registry.Registry) *Controller {
	return &Controller{svc: svc, reg: reg}
}

// SetTimeout overrides how long a command waits for a bulb to confirm.
// A non-positive duration restores the default.
func (c *Controller) SetTimeout(d time.Duration) { c.svc.SetTimeout(d) }

// Execute parses and runs one line of operator input.
func (c *Controller) Execute(ctx context.Context, line string) Result {
	cmd, err := Parse(line)
	if err != nil {
		if err == ErrEmpty {
			return Result{Kind: ResultInfo}
		}
		return Result{Kind: ResultError, Text: err.Error()}
	}
	return c.Run(ctx, cmd)
}

// Run executes an already-parsed command.
func (c *Controller) Run(ctx context.Context, cmd Command) Result {
	switch cmd.Action {
	case ActionList:
		return c.list()
	case ActionHelp:
		return infof("%s", Help(cmd.Text))
	case ActionQuit:
		return Result{Kind: ResultOK, Text: "shutting down", Quit: true}
	}

	// Prefix and name matching stay here. A human is typing; an integration is
	// not, and lights.Service deliberately refuses to guess.
	target, err := c.reg.Resolve(cmd.Target)
	if err != nil {
		return Result{Kind: ResultError, Text: err.Error()}
	}

	switch cmd.Action {
	case ActionInfo:
		return c.info(target)
	case ActionName:
		return c.rename(target, cmd.Text)
	}

	change, res := changeFor(target, cmd)
	if res != nil {
		return *res
	}

	switch err := c.svc.Apply(ctx, target.DeviceID, change); {
	case err == nil:
		return okf("%s: %s", target.Name, describe(cmd))

	case errors.Is(err, bulb.ErrUnconfirmed):
		// The command reached the bulb; it simply has not reported back yet.
		// Calling that a failure would be a lie, and the operator has watched
		// this exact case succeed seconds later.
		return infof("%s: %s sent — the bulb has not confirmed yet, watch the feed",
			target.Name, describe(cmd))

	default:
		return c.renderError(target, err)
	}
}

// renderError turns a typed service error into the terminal's wording. This is
// the whole reason the service returns types rather than strings: Home
// Assistant renders the same errors completely differently.
func (c *Controller) renderError(target bulb.Bulb, err error) Result {
	switch {
	case errors.Is(err, lights.ErrNotAdopted):
		return errf("%s: not adopted yet. run `name %s <a-name>` first", target.DeviceID, target.DeviceID)
	case errors.Is(err, lights.ErrNotConnected):
		return errf("%s: not connected (last seen %s). check the bulb has power",
			target.Name, target.LastSeen.Format("15:04:05"))
	case errors.Is(err, bulb.ErrUnsupported):
		return errf("%s: does not support colour temperature. this bulb accepts `on`, `off`, `bri`", target.Name)
	default:
		var re lights.RangeError
		if errors.As(err, &re) {
			return Result{Kind: ResultError, Text: re.Error()}
		}
		return errf("%s: %v", target.Name, err)
	}
}

// changeFor turns a parsed command into a service change, applying the checks
// that are about the terminal's own vocabulary rather than about the lamp. A
// non-nil Result means the command was refused before it reached the service.
func changeFor(target bulb.Bulb, cmd Command) (lights.Change, *Result) {
	var change lights.Change
	switch cmd.Action {
	case ActionOn:
		on := true
		change.Power = &on
	case ActionOff:
		off := false
		change.Power = &off
	case ActionBrightness:
		if cmd.Number < 0 || cmd.Number > 100 {
			r := errf("brightness must be 0-100, got %d", cmd.Number)
			return change, &r
		}
		pct := uint8(cmd.Number)
		change.Brightness = &pct
	case ActionColorTemp:
		if cmd.Number < 0 || cmd.Number > 100 {
			r := errf("colour temperature must be 0-100 (0 = warmest, 100 = coolest), got %d", cmd.Number)
			return change, &r
		}
		if !target.Capabilities.SupportsColorTemp() {
			r := errf("%s: does not support colour temperature. this bulb accepts `on`, `off`, `bri`", target.Name)
			return change, &r
		}
		pct := uint8(cmd.Number)
		change.ColorTemp = &pct
	case ActionColor:
		if !validColor(cmd.Text) {
			r := errf("colour must be #RRGGBB or a name, got %q", cmd.Text)
			return change, &r
		}
		if !target.Capabilities.SupportsColor() {
			r := errf("%s: does not support colour. this bulb accepts `on`, `off`, `bri`, `temp`", target.Name)
			return change, &r
		}
		// No RGB model has been captured yet, so there is no property mapping to
		// send. Saying so plainly beats reporting a success that did nothing.
		r := errf("%s: colour is not implemented for this bulb's protocol yet. `temp` sets white warmth 0-100", target.Name)
		return change, &r
	}
	return change, nil
}

func describe(cmd Command) string {
	switch cmd.Action {
	case ActionOn:
		return "on"
	case ActionOff:
		return "off"
	case ActionBrightness:
		return fmt.Sprintf("brightness %d", cmd.Number)
	case ActionColorTemp:
		return fmt.Sprintf("temperature %d", cmd.Number)
	default:
		return "done"
	}
}

// rename assigns a name. Naming a discovered bulb adopts it: one verb, because
// a bulb worth keeping is a bulb worth naming.
func (c *Controller) rename(target bulb.Bulb, name string) Result {
	if _, err := Parse(name); err == nil && isVerb(name) {
		return errf("%q is a command name; pick something else so `%s` stays unambiguous", name, name)
	}
	previous := target.Name
	adopted, err := c.svc.Rename(target.DeviceID, name)
	if err != nil {
		return Result{Kind: ResultError, Text: err.Error()}
	}
	if adopted {
		return okf("%s: adopted (was %s)", name, target.DeviceID)
	}
	return okf("%s: renamed from %s", name, previous)
}

func isVerb(s string) bool {
	for _, v := range Verbs {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func (c *Controller) list() Result {
	bulbs := c.reg.List()
	if len(bulbs) == 0 {
		return infof("no bulbs yet. power one on with its cloud hostname pointed at this server")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s %-14s %-13s %-4s %4s %5s  %s\n",
		"NAME", "ID", "STATUS", "PWR", "BRI", "TEMP", "LAST SEEN")
	for _, x := range bulbs {
		name := x.Name
		if !x.Adopted() {
			name = "(unadopted)"
		}
		fmt.Fprintf(&b, "%-14s %-14s %-13s %-4s %4d %5d  %s\n",
			name, x.DeviceID, x.Status, power(x.State.Power),
			x.State.Brightness, x.State.ColorTemp, x.LastSeen.Format("15:04:05"))
	}
	return Result{Kind: ResultInfo, Text: strings.TrimRight(b.String(), "\n")}
}

func power(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (c *Controller) info(x bulb.Bulb) Result {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", x.Name, x.DeviceID)
	fmt.Fprintf(&b, "  status       %s, last seen %s\n", x.Status, x.LastSeen.Format("15:04:05"))
	fmt.Fprintf(&b, "  state        power %s, brightness %d, temperature %d, mode %s\n",
		power(x.State.Power), x.State.Brightness, x.State.ColorTemp, x.State.Mode)
	fmt.Fprintf(&b, "  capabilities %s\n", describeCaps(x.Capabilities))
	if !x.Adopted() {
		fmt.Fprintf(&b, "  adopt with   name %s <a-name>\n", x.DeviceID)
	}
	if x.Desired != nil {
		for _, d := range x.State.Diff(*x.Desired) {
			fmt.Fprintf(&b, "  pending      %s\n", d)
		}
	}
	return Result{Kind: ResultInfo, Text: strings.TrimRight(b.String(), "\n")}
}

func describeCaps(c bulb.Capabilities) string {
	if !c.Known {
		return "undetermined — commands are attempted rather than refused"
	}
	var have []string
	if c.Color {
		have = append(have, "colour")
	}
	if c.ColorTemp {
		have = append(have, "colour temperature")
	}
	have = append(have, fmt.Sprintf("brightness %d-100", max(c.MinBrightness, 1)))
	sort.Strings(have)
	return strings.Join(have, ", ")
}
