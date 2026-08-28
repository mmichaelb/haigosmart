# Home Assistant

Your lamps appear in Home Assistant as ordinary lights: dashboards, automations,
scenes and voice assistants all treat them like any other. The terminal keeps
working — it is where you adopt a new bulb and where you diagnose one that
misbehaves.

## What you need first

**An MQTT broker. You run it; this project does not provide one.**

The usual choice is the Mosquitto add-on:

1. Home Assistant → Settings → Add-ons → Add-on Store → **Mosquitto broker** → Install, then Start.
2. Settings → Devices & Services → the **MQTT** integration should be offered
   automatically; configure it against Mosquitto.
3. Note the broker's address and, if you set them, its username and password.

The lamps never talk to the broker. Only this server does, so nothing here can
put a lamp back on the vendor's cloud, and a broker problem can never stop the
terminal from controlling a lamp.

## Turning it on

```bash
./haigosmartd -mqtt-broker 192.168.1.10:1883 -mqtt-username ha -mqtt-password '…'
```

Leave `-mqtt-broker` unset and nothing changes: the server behaves exactly as it
does without Home Assistant.

Adopted lamps appear within a minute under **Settings → Devices & Services →
MQTT**, with no YAML edited by hand.

## Adopting a lamp

Adoption stays in the terminal, deliberately. A brand-new bulb shows up as
`discovered` and is **not** published to Home Assistant until you name it —
otherwise your house fills with entries nobody can identify.

```text
> list
> name 703e975dc388 headlamp
ok      headlamp: adopted (was 703e975dc388)
```

It appears in Home Assistant moments later.

## What the entity offers

Exactly what the lamp can do, and nothing else. The captured
`aigo_light_cct_v4.0.0` is white-only, so its entity declares brightness and
colour temperature and **no colour wheel** — not because the interface hides
one, but because the entity never claims a colour channel it does not have.

A lamp whose capabilities could not be determined advertises brightness only.
Claiming less than the truth is recoverable; claiming more is not.

Colour temperature runs 2700 K (warmest) to 6500 K (coolest), which is this
hardware's range. `-ct-min-kelvin` and `-ct-max-kelvin` exist for a different
model with different endpoints.

## Naming

Renaming in the terminal updates the *default* name in Home Assistant. If you
have renamed the entity inside Home Assistant, your name wins and is not
overwritten. The device identity never changes, so history, dashboards and
automations survive a rename.

## Availability

Two things have to be true for a lamp to show as available: the server is
running, and that lamp is connected.

- Unplug one lamp and it shows unavailable; the others are unaffected.
- Stop the server and every lamp shows unavailable, including if it crashes or
  loses the network — that is an MQTT last will, which a shutdown handler
  cannot cover.
- Everything recovers on its own. Nothing needs re-adding.

**State is never restored.** After a restart, a lamp shows nothing until it
reports. That is deliberate: a remembered brightness from last week looks
authoritative and may be entirely wrong, and an automation reading it would act
on a fiction.

## When a lamp is in the terminal but not in Home Assistant

Work down this list:

1. **Is it adopted?** `list` in the terminal. A `discovered` lamp is not
   published by design.
2. **Is the bridge connected?** The log carries `connected to the mqtt broker`
   on success, and a specific reason on failure — a rejected password says so
   rather than looking like a network error.
3. **Is Home Assistant's MQTT integration pointed at the same broker?**
4. **Watch the topics.** In Home Assistant: Settings → Devices & Services →
   MQTT → Configure → Listen to a topic → `homeassistant/light/#`. A retained
   discovery config should be there.
5. **Run with `-v`** for the full exchange, including the discovery payloads.

## Two devices, not one

Home Assistant lists the server and each lamp as separate devices — that is
correct, not a duplicate. `via_device` draws a "connected via" relationship on
each lamp's page; it does not nest them in the device list.

The list shows each device's **model**, so you will see "Bulb Server" and
"Tunable White Smart Bulb". The names are `haigosmart` and whatever you called
the lamp, and the raw firmware string is on the lamp's device page as its
software version.

## The "haigosmart" device

The server appears as its own device with a **Status** connectivity sensor, and
every lamp is listed as connected via it. That grouping is why the device
exists: Home Assistant resolves each lamp's parent by identifier, and without a
device declaring it you get an "Unnamed device" placeholder instead.

The Status sensor shows `Connected` while the server is running and
`Disconnected` when it is not — including when it crashes, via the MQTT last
will. It deliberately has no availability of its own, since an entity whose job
is to report the server being gone must not vanish at that exact moment.

## Removing a lamp

Removing a lamp from the registry clears its Home Assistant entry too, rather
than leaving a permanent unavailable device behind.
