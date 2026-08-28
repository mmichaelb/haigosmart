# Contract: Configuration

**Feature**: 002-homeassistant-integration | Spec: FR-019, FR-021, FR-022

The integration is off unless a broker is configured. Running the server exactly as feature
001 did remains fully supported (FR-019).

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-mqtt-broker` | *(empty)* | `host:port` of the owner's broker. **Empty disables the integration entirely** |
| `-mqtt-username` | *(empty)* | Optional broker credentials |
| `-mqtt-password` | *(empty)* | Never logged, never echoed |
| `-mqtt-client-id` | `haigosmart` | Client id presented to the broker |
| `-mqtt-prefix` | `haigosmart` | Base topic for state, availability and commands |
| `-mqtt-discovery-prefix` | `homeassistant` | Home Assistant's discovery prefix |
| `-ct-min-kelvin` | `2700` | Kelvin at the lamp's warmest (percent 0) |
| `-ct-max-kelvin` | `6500` | Kelvin at the lamp's coolest (percent 100) |

The defaults are correct for this hardware: 2700–6500 K, confirmed by the operator. The
lamps report warmth as a percentage and never state the Kelvin endpoints themselves, so the
range is a property of the LEDs rather than of the protocol — the flags exist so a different
Aigo model with different endpoints needs a config change rather than a rebuild. Nobody
running the captured lamp needs to touch them.

## Failure behaviour

| Situation | Result |
|---|---|
| `-mqtt-broker` empty | Integration never starts. Everything else behaves exactly as feature 001 |
| Broker unreachable at startup | Server starts normally, lamps work from the terminal, the client retries with backoff |
| Broker disappears while running | Lamps and terminal unaffected (FR-021); the client reconnects and republishes on its own (FR-022) |
| Broker rejects credentials | Logged clearly, with the broker address and the reason. Retries continue; the terminal is unaffected |
| Bad Kelvin range (min ≥ max) | Refused at startup with a message naming both values — a silently inverted scale would be worse than not starting |

**The lamps never talk to the broker.** Nothing in this feature can put a lamp back on the
vendor's cloud, and no broker problem can stop the terminal from controlling a lamp.
