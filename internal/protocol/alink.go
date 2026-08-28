package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"haigosmart/internal/bulb"
)

// Alink property names used by the Aigo bulbs, from the capture.
const (
	propSwitch     = "LightSwitch"
	propBrightness = "Brightness"
	propColorTemp  = "ColorTemperature"
	propLightMode  = "LightMode"
	propResponse   = "CommonServiceResponse"
)

// Identity is the device identity taken from the MQTT CONNECT packet. The
// username is "{deviceName}&{productKey}" and deviceName is the bulb's MAC,
// stable across reconnects.
type Identity struct {
	DeviceName string
	ProductKey string
}

// DeviceID returns the stable identifier used everywhere above the protocol.
func (i Identity) DeviceID() string { return i.DeviceName }

// IdentityFromConnect extracts the device identity from a CONNECT packet.
func IdentityFromConnect(c Connect) (Identity, error) {
	name, key, ok := strings.Cut(c.Username, "&")
	if ok && name != "" && key != "" {
		return Identity{DeviceName: name, ProductKey: key}, nil
	}
	// Fall back to the client id, which is "{productKey}.{deviceName}|...".
	head, _, _ := strings.Cut(c.ClientID, "|")
	if key, name, ok := strings.Cut(head, "."); ok && key != "" && name != "" {
		return Identity{DeviceName: name, ProductKey: key}, nil
	}
	return Identity{}, fmt.Errorf("protocol: cannot derive device identity from username %q / client id %q", c.Username, c.ClientID)
}

// Topics returns the topic strings for one device.
type Topics struct{ pk, dn string }

// TopicsFor builds the topic set for an identity.
func TopicsFor(id Identity) Topics { return Topics{pk: id.ProductKey, dn: id.DeviceName} }

// CommonService is the topic commands are published to.
func (t Topics) CommonService() string {
	return fmt.Sprintf("/sys/%s/%s/thing/service/CommonService", t.pk, t.dn)
}

// PropertyPostReply is the topic a property post is acknowledged on.
func (t Topics) PropertyPostReply() string {
	return fmt.Sprintf("/sys/%s/%s/thing/event/property/post_reply", t.pk, t.dn)
}

// NTPResponse is the topic the bulb's time-sync request is answered on.
func (t Topics) NTPResponse() string {
	return fmt.Sprintf("/ext/ntp/%s/%s/response", t.pk, t.dn)
}

// Topic classification. The bulb publishes to a handful of well-known suffixes.
const (
	SuffixPropertyPost = "/thing/event/property/post"
	SuffixServiceReply = "/thing/service/CommonService_reply"
	SuffixOTAInform    = "/ota/device/inform/"
	SuffixNTPRequest   = "/request"
)

// PropertyPost is a decoded state report. Only fields the bulb actually sent
// are non-nil, because reports after the first one are deltas.
type PropertyPost struct {
	ID         string
	Power      *bool
	Brightness *uint8
	ColorTemp  *uint8
	Mode       *bulb.Mode
	Seq        string // echoed from the command that caused this change, if any
}

// alinkValue accepts both shapes the bulb uses: a bare scalar in the initial
// full report, and a {"value":…,"time":…} wrapper in every delta afterwards.
type alinkValue struct {
	raw json.RawMessage
}

func (a *alinkValue) UnmarshalJSON(b []byte) error { a.raw = b; return nil }

func (a alinkValue) number() (float64, bool) {
	if len(a.raw) == 0 {
		return 0, false
	}
	var direct float64
	if err := json.Unmarshal(a.raw, &direct); err == nil {
		return direct, true
	}
	var wrapped struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(a.raw, &wrapped); err == nil {
		return wrapped.Value, true
	}
	return 0, false
}

func (a alinkValue) seq() string {
	var wrapped struct {
		Value struct {
			Seq string `json:"seq"`
		} `json:"value"`
	}
	if err := json.Unmarshal(a.raw, &wrapped); err == nil {
		return wrapped.Value.Seq
	}
	return ""
}

// DecodePropertyPost parses a thing.event.property.post payload.
func DecodePropertyPost(payload []byte) (PropertyPost, error) {
	var msg struct {
		ID     string                `json:"id"`
		Params map[string]alinkValue `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return PropertyPost{}, fmt.Errorf("protocol: decoding property post: %w", err)
	}
	post := PropertyPost{ID: msg.ID}
	for name, value := range msg.Params {
		switch name {
		case propSwitch:
			if n, ok := value.number(); ok {
				on := n != 0
				post.Power = &on
			}
		case propBrightness:
			if n, ok := value.number(); ok {
				b := clampPercent(n)
				post.Brightness = &b
			}
		case propColorTemp:
			if n, ok := value.number(); ok {
				t := clampPercent(n)
				post.ColorTemp = &t
			}
		case propLightMode:
			if n, ok := value.number(); ok {
				m := bulb.ModeWhite
				if n != 0 {
					m = bulb.ModeScene
				}
				post.Mode = &m
			}
		case propResponse:
			post.Seq = value.seq()
		}
	}
	return post, nil
}

// Apply folds a delta report onto a previous state, leaving untouched fields
// alone. The bulb's report always wins over any commanded value (FR-019).
func (p PropertyPost) Apply(prev bulb.LightState, at time.Time) bulb.LightState {
	next := prev
	if p.Power != nil {
		next.Power = *p.Power
	}
	if p.Brightness != nil {
		next.Brightness = *p.Brightness
	}
	if p.ColorTemp != nil {
		next.ColorTemp = *p.ColorTemp
	}
	if p.Mode != nil {
		next.Mode = *p.Mode
	}
	next.ReportedAt = at
	return next
}

func clampPercent(n float64) uint8 {
	switch {
	case n < 0:
		return 0
	case n > 100:
		return 100
	default:
		return uint8(n)
	}
}

// EncodePropertyPostReply builds the acknowledgement the real cloud sends back
// for a property post. The bulb tolerates its absence but retries without it.
func EncodePropertyPostReply(id string) []byte {
	reply := map[string]any{
		"code": 200, "data": map[string]any{}, "id": id,
		"message": "success", "method": "thing.event.property.post", "version": "1.0",
	}
	out, _ := json.Marshal(reply) // fixed shape; cannot fail
	return out
}

// EncodeCommand builds a thing.service.CommonService command carrying the given
// properties. params is double-encoded (a JSON string inside a JSON object),
// exactly as the vendor cloud does it.
func EncodeCommand(props map[string]any, now time.Time) (payload []byte, seq string, err error) {
	inner, err := json.Marshal(props)
	if err != nil {
		return nil, "", fmt.Errorf("protocol: encoding command properties: %w", err)
	}
	millis := now.UnixMilli()
	seq = "10000@" + strconv.FormatInt(millis, 10)
	msg := map[string]any{
		"method":  "thing.service.CommonService",
		"id":      strconv.FormatInt(millis%1_000_000_000, 10),
		"version": "1.0.0",
		"params": map[string]any{
			"flag": 0, "method": 0, "params": string(inner), "seq": seq,
		},
	}
	payload, err = json.Marshal(msg)
	if err != nil {
		return nil, "", fmt.Errorf("protocol: encoding command: %w", err)
	}
	return payload, seq, nil
}

// Property is one Alink property assignment.
type Property struct {
	Name  string
	Value any
}

// Map renders the property as the single-entry object a command carries.
func (p Property) Map() map[string]any { return map[string]any{p.Name: p.Value} }

// ChangedProps returns the properties needed to move a bulb from current to
// want, in the order they should be sent — and only the ones that actually
// differ.
//
// Each is sent as its own command. The captured traffic shows the vendor's app
// never bundles: every observed CommonService message carries exactly one
// property, even when the user changed two things at once. A bundled command is
// silently ignored by the hardware, which then never acknowledges it.
func ChangedProps(current, want bulb.LightState, caps bulb.Capabilities) []Property {
	var out []Property

	// Turning on comes first, so the attributes that follow apply to a lit bulb.
	if want.Power && want.Power != current.Power {
		out = append(out, Property{propSwitch, 1})
	}
	if want.Power {
		if want.Brightness > 0 && want.Brightness != current.Brightness {
			out = append(out, Property{propBrightness, int(want.Brightness)})
		}
		if caps.SupportsColorTemp() && want.ColorTemp != current.ColorTemp {
			out = append(out, Property{propColorTemp, int(want.ColorTemp)})
		}
	}
	// Turning off comes last, and alone: adjusting brightness on a bulb that is
	// about to go dark is pointless traffic.
	if !want.Power && want.Power != current.Power {
		out = append(out, Property{propSwitch, 0})
	}
	return out
}

// EncodeNTPResponse answers the bulb's time-sync request.
func EncodeNTPResponse(deviceSendTime string, now time.Time) []byte {
	millis := now.UnixMilli()
	out, _ := json.Marshal(map[string]any{
		"deviceSendTime": deviceSendTime,
		"serverRecvTime": strconv.FormatInt(millis, 10),
		"serverSendTime": strconv.FormatInt(millis, 10),
	})
	return out
}

// DecodeNTPRequest extracts the device's send time from a time-sync request.
func DecodeNTPRequest(payload []byte) string {
	var req struct {
		DeviceSendTime json.RawMessage `json:"deviceSendTime"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return "0"
	}
	return strings.Trim(string(req.DeviceSendTime), `"`)
}

// ServiceReply is a decoded CommonService_reply: the bulb's acknowledgement of
// a command, correlated by the command's id.
type ServiceReply struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

// DecodeServiceReply parses a CommonService_reply payload.
func DecodeServiceReply(payload []byte) (ServiceReply, error) {
	var r ServiceReply
	if err := json.Unmarshal(payload, &r); err != nil {
		return r, fmt.Errorf("protocol: decoding service reply: %w", err)
	}
	return r, nil
}
